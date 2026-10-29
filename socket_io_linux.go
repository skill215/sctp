//go:build linux && !386

package sctp

import (
	"errors"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"
)

func newSocketIO(file *os.File) *socketIO {
	if file == nil {
		return nil
	}
	return &socketIO{file: file, readGate: make(chan struct{}, 1), writeGate: make(chan struct{}, 1), closing: make(chan struct{})}
}

// socketTimeout reads SOL_SOCKET rather than the SOL_SCTP option wrappers.
// socklen_t is 32 bits even on 64-bit and big-endian platforms.
func socketTimeout(fd, option int) (time.Duration, error) {
	var value syscall.Timeval
	size := uint32(unsafe.Sizeof(value))
	_, _, err := getSocketOption(fd, syscall.SOL_SOCKET, uintptr(option), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&size)))
	if err != nil {
		return 0, err
	}
	const max = int64(1<<63 - 1)
	sec, usec := int64(value.Sec), int64(value.Usec)
	if sec < 0 || usec < 0 {
		return 0, syscall.EINVAL
	}
	if sec > max/int64(time.Second) {
		return time.Duration(max), nil
	}
	ns := sec * int64(time.Second)
	if usec > (max-ns)/int64(time.Microsecond) {
		return time.Duration(max), nil
	}
	return time.Duration(ns + usec*int64(time.Microsecond)), nil
}

func (s *socketIO) Control(f func(uintptr)) error {
	select {
	case <-s.closing:
		return net.ErrClosed
	default:
	}
	raw, err := s.file.SyscallConn()
	if err != nil {
		return err
	}
	return raw.Control(f)
}

func (s *socketIO) Read(f func(uintptr) bool) error  { return s.poll(true, f) }
func (s *socketIO) Write(f func(uintptr) bool) error { return s.poll(false, f) }

func (s *socketIO) acquire(gate chan struct{}, deadline time.Time) error {
	select {
	case <-s.closing:
		return net.ErrClosed
	default:
	}
	select {
	case gate <- struct{}{}:
		return nil
	default:
	}
	var expired <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-s.closing:
		return net.ErrClosed
	case <-expired:
		return syscall.EAGAIN
	}
}

func (s *socketIO) poll(read bool, f func(uintptr) bool) error {
	deadline, err := s.deadline(read)
	if err != nil {
		return err
	}
	return s.pollDeadline(read, deadline, f)
}

// deadline snapshots the socket timeout once for a whole public operation.
// A read that consumes notifications reuses it for every readiness wait.
func (s *socketIO) deadline(read bool) (time.Time, error) {
	option := syscall.SO_SNDTIMEO
	if read {
		option = syscall.SO_RCVTIMEO
	}
	var timeout time.Duration
	var optionErr error
	if err := s.Control(func(fd uintptr) { timeout, optionErr = socketTimeout(int(fd), option) }); err != nil {
		return time.Time{}, err
	}
	if optionErr != nil {
		return time.Time{}, optionErr
	}
	if timeout > 0 {
		return time.Now().Add(timeout), nil
	}
	return time.Time{}, nil
}

func (s *socketIO) pollDeadline(read bool, deadline time.Time, f func(uintptr) bool) error {
	gate := s.writeGate
	setDeadline := s.file.SetWriteDeadline
	if read {
		gate, setDeadline = s.readGate, s.file.SetReadDeadline
	}
	if err := s.acquire(gate, deadline); err != nil {
		return err
	}
	defer func() { <-gate }()
	if !deadline.IsZero() {
		if time.Until(deadline) <= 0 {
			return syscall.EAGAIN
		}
		if err := setDeadline(deadline); err != nil {
			return err
		}
		// Clear the per-operation deadline before another reader/writer enters.
		defer setDeadline(time.Time{})
	}
	raw, err := s.file.SyscallConn()
	if err != nil {
		return err
	}
	if read {
		err = raw.Read(f)
	} else {
		err = raw.Write(f)
	}
	if !deadline.IsZero() && errors.Is(err, os.ErrDeadlineExceeded) {
		return syscall.EAGAIN
	}
	return err
}

func (s *socketIO) Close() error {
	// Also release operations queued at a direction gate, outside the poller.
	s.closeOnce.Do(func() { close(s.closing) })
	return s.file.Close()
}
