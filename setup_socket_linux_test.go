//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func acceptQuiescent(t *testing.T, ln *SCTPListener) {
	t.Helper()
	ln.io.deadlineMu.Lock()
	defer ln.io.deadlineMu.Unlock()
	if ln.io.readDeadline.owner != nil || len(ln.io.readGate) != 0 {
		t.Fatal("accept owner retained")
	}
}
func acceptClient(t *testing.T, ln *SCTPListener) *SCTPConn {
	t.Helper()
	a, e := ln.BoundAddr()
	if e != nil {
		t.Fatal(e)
	}
	cfg := SocketConfig{}
	c, e := cfg.DialContext(context.Background(), "sctp4", nil, a)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestAcceptContextIdleCancellationAndReuse(t *testing.T) {
	ln := lifecycleListener(t)
	for range 10 {
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		done := make(chan error, 1)
		var calls atomic.Int32
		var once sync.Once
		go func() {
			c, e := ln.acceptContext(ctx, func(fd, flags int) (int, syscall.Sockaddr, error) {
				calls.Add(1)
				once.Do(func() { close(entered) })
				return syscall.Accept4(fd, flags)
			}, finishMessageConn)
			if c != nil {
				c.Close()
			}
			done <- e
		}()
		deadlineEntered(t, entered)
		// A timer observes idle behavior; explicit owner/gate retirement below is
		// the lifecycle evidence, not timing or a global goroutine count.
		<-time.After(25 * time.Millisecond)
		if calls.Load() != 1 {
			t.Fatal("idle accept periodically woke", calls.Load())
		}
		cancel()
		if e := deadlineResult(t, done); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		acceptQuiescent(t, ln)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := acceptClient(t, ln)
	s, e := ln.AcceptSCTPContext(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cancel()
	ln.Close()
	acceptQuiescent(t, ln)
	s.SetDeadline(time.Now().Add(time.Second))
	c.SetDeadline(time.Now().Add(time.Second))
	if _, e = c.Write([]byte("handoff")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 16)
	if n, e := s.Read(b); e != nil || string(b[:n]) != "handoff" {
		t.Fatal(n, e)
	}
}
func TestAcceptContextPreflightAndQueuedIsolation(t *testing.T) {
	ln := lifecycleListener(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		if c, e := ln.acceptContext(ctx, func(int, int) (int, syscall.Sockaddr, error) { t.Fatal("preflight did I/O"); return -1, nil, nil }, finishMessageConn); c != nil || e == nil {
			t.Fatal(c, e)
		}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() {
		_, e := ln.acceptContext(ctx, func(int, int) (int, syscall.Sockaddr, error) {
			close(entered)
			<-release
			return -1, nil, syscall.EAGAIN
		}, finishMessageConn)
		done <- e
	}()
	deadlineEntered(t, entered)
	queued, qcancel := context.WithCancel(context.Background())
	qdone := make(chan error, 1)
	go func() { _, e := ln.AcceptSCTPContext(queued); qdone <- e }()
	qcancel()
	if e := deadlineResult(t, qdone); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	stop()
	close(release)
	if e := deadlineResult(t, done); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	acceptQuiescent(t, ln)
	c := acceptClient(t, ln)
	defer c.Close()
	s, e := ln.AcceptSCTP()
	if e != nil {
		t.Fatal("old hook poisoned next owner", e)
	}
	s.Close()
}
func TestAcceptContextPublicationBarriers(t *testing.T) {
	for _, stage := range []string{"after-accept", "finish", "finish-error", "close", "deadline", "extend", "clear"} {
		t.Run(stage, func(t *testing.T) {
			ln := lifecycleListener(t)
			client := acceptClient(t, ln)
			defer client.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var child *SCTPConn
			var fd int
			closer := make(chan error, 1)
			accept := func(l, f int) (int, syscall.Sockaddr, error) {
				n, a, e := syscall.Accept4(l, f)
				fd = n
				if e == nil && stage == "after-accept" {
					cancel()
				}
				return n, a, e
			}
			finish := func(c *SCTPConn, m *MessageOptions) error {
				child = c
				switch stage {
				case "finish":
					cancel()
				case "finish-error":
					return syscall.EPROTO
				case "close":
					go func() { closer <- ln.Close() }()
					<-ln.io.closing
				case "deadline", "extend", "clear":
					if e := ln.SetDeadline(time.Now().Add(-time.Second)); e != nil {
						t.Error(e)
					}
					if stage == "extend" {
						ln.SetDeadline(time.Now().Add(time.Hour))
					}
					if stage == "clear" {
						ln.SetDeadline(time.Time{})
					}
				}
				return finishMessageConn(c, m)
			}
			c, e := ln.acceptContext(ctx, accept, finish)
			if stage == "close" {
				if ce := deadlineResult(t, closer); ce != nil {
					t.Fatal(ce)
				}
			}
			acceptQuiescent(t, ln)
			if stage == "extend" || stage == "clear" {
				if e != nil || c == nil {
					t.Fatal(c, e)
				}
				c.Close()
				return
			}
			want := context.Canceled
			switch stage {
			case "close":
				want = net.ErrClosed
			case "deadline":
				want = os.ErrDeadlineExceeded
			case "finish-error":
				want = syscall.EPROTO
			}
			if c != nil || !errors.Is(e, want) {
				t.Fatal(c, e, want)
			}
			if child != nil && child.fd() != -1 {
				t.Fatal("child not retired")
			}
			if _, e := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE); !errors.Is(e, syscall.EBADF) {
				t.Fatal("fd leaked", e)
			}
		})
	}
}
func TestAcceptContextDeadlinesAndClose(t *testing.T) {
	ln := lifecycleListener(t)
	for range 2 {
		ln.SetDeadline(time.Now().Add(-time.Second))
		if c, e := ln.AcceptSCTPContext(context.Background()); c != nil || !errors.Is(e, os.ErrDeadlineExceeded) {
			t.Fatal(c, e)
		}
	}
	ln.SetDeadline(time.Time{})
	entered := make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	go func() {
		_, e := ln.acceptContext(context.Background(), func(fd, f int) (int, syscall.Sockaddr, error) {
			once.Do(func() { close(entered) })
			return syscall.Accept4(fd, f)
		}, finishMessageConn)
		done <- e
	}()
	deadlineEntered(t, entered)
	ln.SetDeadline(time.Now().Add(-time.Second))
	if e := deadlineResult(t, done); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	acceptQuiescent(t, ln)
	ln.SetDeadline(time.Time{})
	go func() { _, e := ln.AcceptSCTPContext(context.Background()); done <- e }()
	ln.Close()
	if e := deadlineResult(t, done); !errors.Is(e, net.ErrClosed) {
		t.Fatal(e)
	}
	acceptQuiescent(t, ln)
}
func TestSocketTimeoutsAndBoundSnapshot(t *testing.T) {
	c, _ := lifecyclePair(t)
	if e := c.SetSocketTimeouts(time.Second, 2*time.Second); e != nil {
		t.Fatal(e)
	}
	if e := c.SetSocketTimeouts(0, -1); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	if e := c.SetSocketTimeouts(0, time.Duration(1<<63-1)); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	c.withFD(func(fd int) error {
		r, e := socketTimeout(fd, syscall.SO_RCVTIMEO)
		if e != nil || r != time.Second {
			t.Fatal(r, e)
		}
		return nil
	})
	public := time.Now().Add(time.Hour)
	c.SetDeadline(public)
	if e := c.SetSocketTimeouts(0, 0); e != nil {
		t.Fatal(e)
	}
	c.withFD(func(fd int) error {
		for _, o := range []int{syscall.SO_RCVTIMEO, syscall.SO_SNDTIMEO} {
			v, e := socketTimeout(fd, o)
			if e != nil || v != 0 {
				t.Fatal(v, e)
			}
		}
		return nil
	})
	if !c.io.readDeadline.deadline.Equal(public) {
		t.Fatal("standard deadline changed")
	}
	for _, fail := range []int{1, 2} {
		calls := 0
		e := setSocketTimeoutsFD(0, syscall.Timeval{}, syscall.Timeval{}, func(int, int, int, *syscall.Timeval) error {
			calls++
			if calls == fail {
				return syscall.EPERM
			}
			return nil
		})
		var update *SocketTimeoutUpdateError
		if !errors.As(e, &update) || update.ReadApplied != (fail == 2) || !errors.Is(e, syscall.EPERM) {
			t.Fatal(e)
		}
	}
	for _, a := range []*SCTPAddr{nil, {}, {IPAddrs: []net.IPAddr{{IP: net.IPv4zero}}}, {IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}, {IP: net.IPv4(127, 0, 0, 2)}}}} {
		ln, e := ListenSCTP("sctp4", a)
		if e != nil {
			t.Fatal(e)
		}
		b, e := ln.BoundAddr()
		if e != nil || b.Port == 0 || len(b.IPAddrs) == 0 {
			t.Fatal(b, e)
		}
		if a == nil || len(a.IPAddrs) == 0 || a.IPAddrs[0].IP.IsUnspecified() {
			if !b.IPAddrs[0].IP.IsUnspecified() {
				t.Fatal("wildcard enumerated", b)
			}
		}
		saved := b.String()
		b.Port = 1
		b.IPAddrs[0].IP[0] ^= 255
		b.IPAddrs[0].Zone = "changed"
		again, _ := ln.BoundAddr()
		if again.String() != saved {
			t.Fatal("snapshot aliased")
		}
		if a != nil && a.Port != 0 {
			t.Fatal("input mutated")
		}
		ln.Close()
	}
	if _, e := (&SCTPListener{}).BoundAddr(); !errors.Is(e, ErrBoundAddrUnavailable) {
		t.Fatal(e)
	}
}

func TestAcceptContextConcurrentCallersAndTimeoutSnapshot(t *testing.T) {
	ln := lifecycleListener(t)
	entered := make(chan struct{})
	first := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	go func() {
		_, e := ln.acceptContext(ctx, func(fd, f int) (int, syscall.Sockaddr, error) {
			once.Do(func() { close(entered) })
			return syscall.Accept4(fd, f)
		}, finishMessageConn)
		first <- e
	}()
	deadlineEntered(t, entered)
	second := make(chan *SCTPConn, 1)
	errs := make(chan error, 1)
	go func() { c, e := ln.AcceptSCTPContext(context.Background()); second <- c; errs <- e }()
	cancel()
	if e := deadlineResult(t, first); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	client := acceptClient(t, ln)
	defer client.Close()
	if e := deadlineResult(t, errs); e != nil {
		t.Fatal(e)
	}
	c := <-second
	if c == nil {
		t.Fatal("missing second result")
	}
	c.Close()
	acceptQuiescent(t, ln)
	// Snapshot S at entry, even when the socket's value changes inside a retry.
	if e := ln.io.Control(func(fd uintptr) {
		tv := syscall.NsecToTimeval(int64(20 * time.Millisecond))
		if e := syscall.SetsockoptTimeval(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); e != nil {
			t.Error(e)
		}
	}); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	_, e := ln.acceptContext(context.Background(), func(fd, f int) (int, syscall.Sockaddr, error) {
		tv := syscall.NsecToTimeval(int64(time.Second))
		if e := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); e != nil {
			t.Error(e)
		}
		return -1, nil, syscall.EINTR
	}, finishMessageConn)
	if !errors.Is(e, ErrSocketTimeout) || !errors.Is(e, syscall.EAGAIN) || time.Since(start) > 500*time.Millisecond {
		t.Fatal(e, time.Since(start))
	}
	acceptQuiescent(t, ln)
}
func TestAcceptContextClosePrecedence(t *testing.T) {
	ln := lifecycleListener(t)
	ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, e := ln.AcceptSCTPContext(ctx); c != nil || !errors.Is(e, net.ErrClosed) {
		t.Fatal(c, e)
	}
}
func TestAcceptContextFaultsAndImportedSnapshot(t *testing.T) {
	ln := lifecycleListener(t)
	for _, fail := range []error{syscall.EINVAL, nil} {
		c, e := ln.acceptContext(context.Background(), func(int, int) (int, syscall.Sockaddr, error) { return -1, nil, fail }, finishMessageConn)
		if c != nil || e == nil {
			t.Fatal(c, e)
		}
		if fail != nil && !errors.Is(e, fail) {
			t.Fatal(e)
		}
		acceptQuiescent(t, ln)
	}
	imported, e := FileListener(ln.file)
	if e != nil {
		t.Fatal(e)
	}
	defer imported.Close()
	if _, e := imported.BoundAddr(); !errors.Is(e, ErrBoundAddrUnavailable) {
		t.Fatal(e)
	}
}
