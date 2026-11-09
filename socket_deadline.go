package sctp

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"time"
)

// ErrSocketTimeout identifies a fixed per-operation SO_RCVTIMEO/SO_SNDTIMEO
// expiry. Legacy socket calls still return EAGAIN; context-aware internal calls
// retain both identities. Standard deadlines return os.ErrDeadlineExceeded.
var ErrSocketTimeout = errors.New("sctp socket timeout")

type socketTimeoutError struct{}

func (socketTimeoutError) Error() string   { return ErrSocketTimeout.Error() }
func (socketTimeoutError) Unwrap() error   { return syscall.EAGAIN }
func (socketTimeoutError) Is(e error) bool { return e == ErrSocketTimeout }
func (socketTimeoutError) Timeout() bool   { return true }
func (socketTimeoutError) Temporary() bool { return true }

type socketOperation struct {
	ctx     context.Context
	timeout time.Time
}
type socketDirection struct {
	deadline time.Time
	changed  chan struct{}
	owner    *socketOperation // unique operation identity prevents stale-hook interference
}

func (s *socketIO) direction(read bool) *socketDirection {
	if read {
		return &s.readDeadline
	}
	return &s.writeDeadline
}
func sooner(a, b time.Time) time.Time {
	if a.IsZero() || !b.IsZero() && b.Before(a) {
		return b
	}
	return a
}
func (s *socketIO) closedLocked() error {
	select {
	case <-s.closing:
		if s.deadlineErr != nil {
			return errors.Join(net.ErrClosed, s.deadlineErr)
		}
		return net.ErrClosed
	default:
		return nil
	}
}
func (s *socketIO) stopLocked(err error) {
	if s.deadlineErr == nil {
		s.deadlineErr = err
	}
	s.closeOnce.Do(func() { close(s.closing) })
}
func (s *socketIO) reasonLocked(d *socketDirection, op *socketOperation, now time.Time) error {
	if e := s.closedLocked(); e != nil {
		return e
	}
	if e := op.ctx.Err(); e != nil {
		return e
	}
	if c, ok := op.ctx.Deadline(); ok && !now.Before(c) {
		return context.DeadlineExceeded
	}
	end := sooner(d.deadline, op.timeout)
	if !end.IsZero() && !now.Before(end) {
		if !d.deadline.IsZero() && end.Equal(d.deadline) {
			return os.ErrDeadlineExceeded
		}
		return socketTimeoutError{}
	}
	return nil
}
func (s *socketIO) installLocked(read bool) error {
	d := s.direction(read)
	end := d.deadline
	if op := d.owner; op != nil {
		end = sooner(end, op.timeout)
		if c, ok := op.ctx.Deadline(); ok {
			end = sooner(end, c)
		}
		if op.ctx.Err() != nil {
			end = time.Unix(1, 0)
		}
	}
	var e error
	if read {
		e = s.file.SetReadDeadline(end)
	} else {
		e = s.file.SetWriteDeadline(end)
	}
	if e != nil {
		s.stopLocked(e)
	}
	return e
}
func (s *socketIO) changeLocked(d *socketDirection) {
	if d.changed != nil {
		close(d.changed)
	}
	d.changed = make(chan struct{})
}
func (s *socketIO) setDeadlines(read, write bool, t time.Time) error {
	s.deadlineMu.Lock()
	if e := s.closedLocked(); e != nil {
		s.deadlineMu.Unlock()
		return e
	}
	if read {
		s.readDeadline.deadline = t
		s.changeLocked(&s.readDeadline)
	}
	if write {
		s.writeDeadline.deadline = t
		s.changeLocked(&s.writeDeadline)
	}
	var e error
	if read {
		e = s.installLocked(true)
	}
	if write && e == nil {
		e = s.installLocked(false)
	}
	s.deadlineMu.Unlock()
	if e != nil {
		s.file.Close()
	} // never close while holding state lock/poller callback
	return e
}

// acquireContext charges queue time to the original socket-timeout snapshot and
// observes mutable public deadlines. Each caller owns only its timer, not a worker.
func (s *socketIO) acquireContext(read bool, op *socketOperation, gate chan struct{}) error {
	for {
		s.deadlineMu.Lock()
		d := s.direction(read)
		e := s.reasonLocked(d, op, time.Now())
		if e != nil {
			s.deadlineMu.Unlock()
			return e
		}
		select {
		case gate <- struct{}{}:
			s.deadlineMu.Unlock()
			return nil
		default:
		}
		if d.changed == nil {
			d.changed = make(chan struct{})
		}
		changed := d.changed
		end := sooner(d.deadline, op.timeout)
		if c, ok := op.ctx.Deadline(); ok {
			end = sooner(end, c)
		}
		s.deadlineMu.Unlock()
		var timer *time.Timer
		var expiry <-chan time.Time
		if !end.IsZero() {
			timer = time.NewTimer(time.Until(end))
			expiry = timer.C
		}
		acquired := false
		select {
		case gate <- struct{}{}:
			acquired = true
		case <-changed:
		case <-op.ctx.Done():
		case <-s.closing:
		case <-expiry:
		}
		if timer != nil {
			timer.Stop()
		}
		if acquired {
			return nil
		} // caller rechecks state before any syscall
	}
}

// pollContext is the common waiting primitive for the future message entrypoints.
// f must perform bounded nonblocking work and return false on EINTR/EAGAIN.
// A true result is committed kernel progress/error; later cancellation cannot erase it.
func (s *socketIO) pollContext(ctx context.Context, read bool, timeout time.Time, f func(uintptr) bool) (err error) {
	if ctx == nil {
		return syscall.EINVAL
	}
	op := &socketOperation{ctx: ctx, timeout: timeout}
	gate := s.writeGate
	if read {
		gate = s.readGate
	}
	if e := s.acquireContext(read, op, gate); e != nil {
		return e
	}
	s.deadlineMu.Lock()
	d := s.direction(read)
	d.owner = op
	err = s.reasonLocked(d, op, time.Now())
	if err == nil {
		err = s.installLocked(read)
	}
	s.deadlineMu.Unlock()
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(finished)
		s.deadlineMu.Lock()
		var e error
		if d.owner == op && s.closedLocked() == nil {
			e = s.installLocked(read)
		}
		s.deadlineMu.Unlock()
		if e != nil {
			s.file.Close()
		}
	})
	defer func() {
		if !stop() {
			<-finished
		}
		s.deadlineMu.Lock()
		d.owner = nil
		var restore error
		if s.closedLocked() == nil {
			restore = s.installLocked(read)
		}
		failed := s.deadlineErr != nil
		s.deadlineMu.Unlock()
		if restore != nil || failed {
			s.file.Close()
		}
		<-gate
	}()
	if err != nil {
		return err
	}
	raw, e := s.file.SyscallConn()
	if e != nil {
		return e
	}
	for {
		var admittedErr error
		committed := false
		call := func(fd uintptr) bool {
			s.deadlineMu.Lock()
			admittedErr = s.reasonLocked(d, op, time.Now())
			s.deadlineMu.Unlock()
			if admittedErr != nil {
				return true
			}
			committed = f(fd)
			return committed
		}
		if read {
			e = raw.Read(call)
		} else {
			e = raw.Write(call)
		}
		if committed {
			return nil
		}
		if admittedErr != nil {
			return admittedErr
		}
		s.deadlineMu.Lock()
		err = s.reasonLocked(d, op, time.Now())
		if err == nil && errors.Is(e, os.ErrDeadlineExceeded) {
			err = s.installLocked(read)
		}
		s.deadlineMu.Unlock()
		if err != nil {
			return err
		}
		if !errors.Is(e, os.ErrDeadlineExceeded) {
			return e
		}
		// A stale poll timeout is not a committed timeout if D was extended/cleared.
	}
}
