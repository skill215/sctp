//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"net"
	"os"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestContextDialRealSCTPAndPostPublicationCancel(t *testing.T) {
	ln := lifecycleListener(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	remote, e := ResolveSCTPAddrContext(ctx, "sctp", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	local := &SCTPAddr{}
	saved := cloneSCTPAddr(remote)
	var handled atomic.Int32
	cfg := SocketConfig{InitMsg: InitMsg{NumOstreams: 3}, NotificationHandler: func([]byte) error { handled.Add(1); return nil }}
	client, e := cfg.DialContext(ctx, "sctp", local, remote)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	server, e := ln.AcceptSCTP()
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	if len(local.IPAddrs) != 0 || !reflect.DeepEqual(saved, remote) {
		t.Fatal("caller address mutated")
	}
	cancel() // no stale dial hook/deadline may affect this connection
	remote.IPAddrs[0].IP[0] ^= 255
	cfg.NotificationHandler = nil
	if e = client.SetDeadline(time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e = client.Write([]byte("published")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 64)
	server.SetReadDeadline(time.Now().Add(time.Second))
	if n, e := server.Read(b); e != nil || string(b[:n]) != "published" {
		t.Fatal(n, e)
	}
	if _, e = server.Write([]byte("answer")); e != nil {
		t.Fatal(e)
	}
	if n, e := client.Read(b); e != nil || string(b[:n]) != "answer" {
		t.Fatal(n, e)
	}
	if client.notificationHandler == nil {
		t.Fatal("config not copied")
	}
}
func TestContextDialValidationBeforeFDAndControl(t *testing.T) {
	called := false
	cfg := SocketConfig{Control: func(string, string, syscall.RawConn) error { called = true; return nil }}
	if c, e := cfg.DialContext(context.Background(), "sctp", nil, &SCTPAddr{Port: 9}); c != nil || !errors.Is(e, ErrInvalidConfig) || called {
		t.Fatal(c, e, called)
	}
	var nilCfg *SocketConfig
	if _, e := nilCfg.DialContext(context.Background(), "sctp", nil, nil); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	cfg = SocketConfig{}
	for _, addr := range []*SCTPAddr{nil, {Port: -1}, {Port: 65536}, {IPAddrs: []net.IPAddr{{IP: net.IP{1, 2}}}}} {
		if c, e := cfg.DialContext(context.Background(), "sctp", nil, addr); c != nil || e == nil {
			t.Fatal(c, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, e := cfg.DialContext(ctx, "sctp", nil, &SCTPAddr{Port: 9}); c != nil || !errors.Is(e, context.Canceled) {
		t.Fatal(c, e)
	}
}
func TestContextDialRefusalAndOwnedFailureCleanup(t *testing.T) {
	// Hold a bound but non-listening SCTP port; no race with another process taking it.
	fd, e := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
	if e != nil {
		t.Fatal(e)
	}
	defer syscall.Close(fd)
	if e = syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); e != nil {
		t.Fatal(e)
	}
	bound, e := syscall.Getsockname(fd)
	if e != nil {
		t.Fatal(e)
	}
	remote := &SCTPAddr{Port: bound.(*syscall.SockaddrInet4).Port, IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}}
	// Warm runtime poller resources before counting fd ownership.
	warm, other := lifecyclePair(t)
	warm.Close()
	other.Close()
	before, e := os.ReadDir("/proc/self/fd")
	if e != nil {
		t.Fatal(e)
	}
	cfg := SocketConfig{}
	for j := 0; j < 20; j++ {
		// A pre-connect bind failure must use the same owner cleanup as refusal.
		if c, e := cfg.DialContext(context.Background(), "sctp", remote, remote); c != nil || !errors.Is(e, syscall.EADDRINUSE) {
			if c != nil {
				c.Close()
			}
			t.Fatal("bind failure", e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, e := cfg.DialContext(ctx, "sctp", nil, remote)
		cancel()
		if c != nil {
			c.Close()
			t.Fatal("refusal returned conn")
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			t.Fatal(e)
		}
	}
	after, e := os.ReadDir("/proc/self/fd")
	if e != nil {
		t.Fatal(e)
	}
	if len(after) != len(before) {
		t.Fatalf("fd leak: %d -> %d", len(before), len(after))
	}
}
func TestContextDialPendingWaitCancellation(t *testing.T) {
	// Model pending establishment with an unconnected real nonblocking SCTP socket.
	// This isolates the waiting/cancellation gate, not remote packet-loss behavior.
	fd, e := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
	if e != nil {
		t.Fatal(e)
	}
	conn := NewSCTPConn(fd, nil)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e = waitContextConnect(ctx, conn.io); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	// Poll cancellation retired its hook and did not close this shared owner.
	if e = conn.io.Control(func(uintptr) {}); e != nil {
		t.Fatal(e)
	}
}
func TestContextDialPublicationGate(t *testing.T) {
	client, server := lifecyclePair(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := publishContextDial(ctx, client); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	// dialSCTPContext owns failure cleanup until publication; no connection is
	// exposed by that path. The helper's separate success point is testable here.
	live, cancelLive := context.WithCancel(context.Background())
	if e := publishContextDial(live, client); e != nil {
		t.Fatal(e)
	}
	cancelLive()
	if _, e := client.Write([]byte("after-handoff")); e != nil {
		t.Fatal(e)
	}
	server.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 32)
	if n, e := server.Read(b); e != nil || string(b[:n]) != "after-handoff" {
		t.Fatal(n, e)
	}
}
func TestContextDialConcurrentAcceptCancellation(t *testing.T) {
	ln := lifecycleListener(t)
	cfg := SocketConfig{}
	for j := 0; j < 20; j++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		accepted := make(chan *SCTPConn, 1)
		go func() {
			s, e := ln.AcceptSCTP()
			if e != nil {
				accepted <- nil
				return
			}
			cancel()
			accepted <- s
		}()
		c, e := cfg.DialContext(ctx, "sctp", nil, ln.Addr().(*SCTPAddr))
		cancel()
		select {
		case s := <-accepted:
			if s == nil {
				t.Fatal("accept failed")
			}
			s.Close()
		case <-time.After(2 * time.Second):
			ln.Close()
			t.Fatal("accept did not retire")
		}
		if e != nil && !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		if e == nil && c == nil {
			t.Fatal("nil success")
		}
		if e != nil && c != nil {
			c.Close()
			t.Fatal("connection on failed publication")
		}
		if c != nil {
			c.Close()
		}
	}
}
