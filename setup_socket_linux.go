//go:build linux && !386

package sctp

import (
	"context"
	"net"
	"syscall"
	"time"
)

// SetDeadline affects current and future accepts. Zero clears it. The deadline
// is never inherited by a returned connection.
func (ln *SCTPListener) SetDeadline(t time.Time) error {
	if ln == nil || ln.io == nil {
		return syscall.EINVAL
	}
	return ln.io.setDeadlines(true, false, t)
}

// AcceptSCTPContext waits without periodic polling or a background accept worker.
// A single read owner covers accept4, child setup and publication. Close, context
// and deadline observed at publication discard the child; later cancellation
// cannot affect a published connection. All started hooks retire before return.
func (ln *SCTPListener) AcceptSCTPContext(ctx context.Context) (*SCTPConn, error) {
	return ln.acceptContext(ctx, syscall.Accept4, finishMessageConn)
}

// Per-call seams keep lifecycle fault injection out of global production state.
func (ln *SCTPListener) acceptContext(ctx context.Context, accept func(int, int) (int, syscall.Sockaddr, error), finish func(*SCTPConn, *MessageOptions) error) (*SCTPConn, error) {
	if ctx == nil {
		return nil, ErrInvalidConfig
	}
	if ln == nil || ln.io == nil {
		return nil, syscall.EINVAL
	}
	ln.io.deadlineMu.Lock()
	entryErr := ln.io.reasonLocked(&ln.io.readDeadline, &socketOperation{ctx: ctx}, time.Now())
	ln.io.deadlineMu.Unlock()
	if entryErr != nil {
		return nil, entryErr
	}
	timeout, e := ln.io.deadline(true)
	if e != nil {
		return nil, e
	}
	var conn *SCTPConn
	var actual error
	e = ln.io.pollContext(ctx, true, timeout, func(fd uintptr) bool {
		child, _, err := accept(int(fd), syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK)
		if err == syscall.EAGAIN || err == syscall.EINTR {
			return false
		}
		if err != nil {
			actual = err
			return true
		}
		conn = NewSCTPConn(child, ln.notificationHandler)
		conn.singleAssociation = true
		actual = conn.initErr
		if actual == nil {
			actual = ln.io.messageAdmission(true)
		}
		if actual == nil && ln.zeroTimeouts {
			actual = conn.SetSocketTimeouts(0, 0)
		}
		if actual == nil {
			actual = finish(conn, ln.messages)
		}
		// Same lock as Close and SetDeadline. Successful accept4 alone is not the
		// commit point. The read owner remains installed until this decision.
		ln.io.deadlineMu.Lock()
		stopped := ln.io.reasonLocked(&ln.io.readDeadline, ln.io.readDeadline.owner, time.Now())
		if stopped != nil {
			actual = stopped
		}
		ln.io.deadlineMu.Unlock()
		if actual != nil {
			conn.Close()
			conn = nil
		}
		return true
	})
	if e != nil {
		return nil, e
	}
	return conn, actual
}

// BoundAddr returns an owned copy of the explicit binding and assigned port,
// including wildcard and zones. This is not a routable-address advertisement.
// It remains available after Close. FileListener returns ErrBoundAddrUnavailable.
func (ln *SCTPListener) BoundAddr() (*SCTPAddr, error) {
	if ln == nil || ln.bound == nil {
		return nil, ErrBoundAddrUnavailable
	}
	return cloneSCTPAddr(ln.bound), nil
}
func boundAddrFD(fd int, input *SCTPAddr) (*SCTPAddr, error) {
	a, e := syscall.Getsockname(fd)
	if e != nil {
		return nil, e
	}
	result := cloneSCTPAddr(input)
	if result == nil {
		result = &SCTPAddr{}
	}
	switch a := a.(type) {
	case *syscall.SockaddrInet4:
		result.Port = a.Port
		if len(result.IPAddrs) == 0 {
			result.IPAddrs = []net.IPAddr{{IP: append(net.IP(nil), net.IPv4zero...)}}
		}
	case *syscall.SockaddrInet6:
		result.Port = a.Port
		if len(result.IPAddrs) == 0 {
			result.IPAddrs = []net.IPAddr{{IP: append(net.IP(nil), net.IPv6zero...)}}
		}
	default:
		return nil, ErrInvalidConfig
	}
	return result, nil
}

func timeoutValue(d time.Duration) (syscall.Timeval, error) {
	// NsecToTimeval rounds upward using +999; reject arithmetic/ABI overflow
	// before either option is modified (including 32-bit timeval targets).
	if d < 0 || d > time.Duration(1<<63-1)-999 {
		return syscall.Timeval{}, ErrInvalidConfig
	}
	tv := syscall.NsecToTimeval(int64(d))
	if int64(tv.Sec) != (int64(d)+999)/1e9 {
		return syscall.Timeval{}, ErrInvalidConfig
	}
	return tv, nil
}

// SetSocketTimeouts changes SO_RCVTIMEO/SO_SNDTIMEO, not standard deadlines.
// Zero clears; negative or unrepresentable durations fail before mutation.
// Intended for exclusive setup/takeover: active operations retain their snapshot.
func (c *SCTPConn) SetSocketTimeouts(read, write time.Duration) error {
	r, e := timeoutValue(read)
	if e != nil {
		return e
	}
	w, e := timeoutValue(write)
	if e != nil {
		return e
	}
	return c.withFD(func(fd int) error { return setSocketTimeoutsFD(fd, r, w, syscall.SetsockoptTimeval) })
}
func setSocketTimeoutsFD(fd int, r, w syscall.Timeval, set func(int, int, int, *syscall.Timeval) error) error {
	if e := set(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &r); e != nil {
		return &SocketTimeoutUpdateError{Option: "SO_RCVTIMEO", Err: e}
	}
	if e := set(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &w); e != nil {
		return &SocketTimeoutUpdateError{Option: "SO_SNDTIMEO", ReadApplied: true, Err: e}
	}
	return nil
}

func listenSetupContext(ctx context.Context, network string, a *SCTPAddr, cfg SocketConfig) (*SCTPListener, error) {
	ln, e := listenSCTPContextConfig(ctx, true, network, a, cfg.InitMsg, nil, nil, cfg.Messages)
	if e != nil {
		return nil, e
	}
	ln.zeroTimeouts = true
	// Fresh sockets default to zero; explicitly enforce this before handoff.
	var actual error
	e = ln.io.Control(func(fd uintptr) {
		actual = setSocketTimeoutsFD(int(fd), syscall.Timeval{}, syscall.Timeval{}, syscall.SetsockoptTimeval)
	})
	if e == nil {
		e = actual
	}
	if e == nil {
		e = contextError(ctx)
	}
	if e != nil {
		ln.Close()
		return nil, e
	}
	return ln, nil
}
