//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestNotificationOptionReadbackMismatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		opt, value int
	}{
		{"nodelay", SCTP_NODELAY, 0}, {"interleave", messageFragmentInterleave, 1}, {"fragmentation", SCTP_DISABLE_FRAGMENTS, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := messagePair(t, 0)
			notificationSetInt(t, c, SOL_SCTP, tc.opt, tc.value)
			actual, e := c.GetMessageOptions()
			if e != nil || actual.Options == *c.messages {
				t.Fatal(actual, e)
			}
			if e = c.withFD(func(fd int) error { return verifyMessageFD(fd, *c.messages) }); !errors.Is(e, ErrInvalidConfig) {
				t.Fatal(e)
			}
		})
	}
	t.Run("events", func(t *testing.T) {
		c, _ := messagePair(t, SCTP_EVENT_ASSOCIATION)
		var zero [10]byte
		if e := c.withFD(func(fd int) error {
			_, _, e := setsockopt(fd, SCTP_EVENTS, uintptr(unsafe.Pointer(&zero[0])), 10)
			return e
		}); e != nil {
			t.Fatal(e)
		}
		actual, e := c.GetMessageOptions()
		if e != nil || actual.Options.ReceiveInfo != ReceiveInfoUnspecified || actual.Options.Events != 0 {
			t.Fatal(actual, e)
		}
		if e = c.withFD(func(fd int) error { return verifyMessageFD(fd, *c.messages) }); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
	})
}
func TestNotificationOptionUnsupportedPreservesErrno(t *testing.T) {
	// A real non-SCTP descriptor must not report configured capabilities.
	fd, e := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_UDP)
	if e != nil {
		t.Fatal(e)
	}
	defer syscall.Close(fd)
	m := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV}
	e = configureMessageFD(fd, m)
	if !errors.Is(e, syscall.ENOPROTOOPT) && !errors.Is(e, syscall.EOPNOTSUPP) {
		t.Fatal(e)
	}
	if _, e = readMessageOptionsFD(fd); !errors.Is(e, syscall.ENOPROTOOPT) && !errors.Is(e, syscall.EOPNOTSUPP) {
		t.Fatal(e)
	}
	if e = configureMessageFD(-1, m); !errors.Is(e, syscall.EBADF) {
		t.Fatal(e)
	}
	if _, e = readMessageOptionsFD(-1); !errors.Is(e, syscall.EBADF) {
		t.Fatal(e)
	}
}
func TestNotificationAcceptMismatchClosesOnlyCandidate(t *testing.T) {
	m := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, NoDelay: true, AllowFragmentation: true}
	cfg := SocketConfig{Messages: &m}
	ln, e := cfg.Listen("sctp4", &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	alter := func(value int) {
		t.Helper()
		var setErr error
		if e := ln.io.Control(func(fd uintptr) { setErr = syscall.SetsockoptInt(int(fd), SOL_SCTP, SCTP_NODELAY, value) }); e != nil {
			t.Fatal(e)
		}
		if setErr != nil {
			t.Fatal(setErr)
		}
	}
	// Deliberately corrupt raw inheritance; the public API cannot do this.
	alter(0)
	before, e := os.ReadDir("/proc/self/fd")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, e := cfg.DialContext(ctx, "sctp4", nil, ln.Addr().(*SCTPAddr))
		cancel()
		if e != nil {
			t.Fatal(e)
		}
		accepted, e := ln.AcceptSCTP()
		if accepted != nil || !errors.Is(e, ErrInvalidConfig) {
			c.Close()
			t.Fatal(accepted, e)
		}
		c.Close()
	}
	after, e := os.ReadDir("/proc/self/fd")
	if e != nil {
		t.Fatal(e)
	}
	if len(after) > len(before) {
		t.Fatal("rejected accept leaked descriptors", len(before), len(after))
	}
	alter(1) // Listener must still work after all20 rejected candidates.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, e := cfg.DialContext(ctx, "sctp4", nil, ln.Addr().(*SCTPAddr))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s, e := ln.AcceptSCTP()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if state, e := s.GetMessageOptions(); e != nil || state.Options != m {
		t.Fatal(state, e)
	}
	if _, e = c.WriteMsgContext(ctx, []byte("still-open"), SndInfo{}); e != nil {
		t.Fatal(e)
	}
	_, b, o := messageBuffers(s)
	d := make([]byte, 20)
	r, e := s.ReadEvent(ctx, d, b, o)
	if e != nil || r.Kind != ReadData || string(d[:r.N]) != "still-open" {
		t.Fatal(r, e, string(d[:r.N]))
	}
}
