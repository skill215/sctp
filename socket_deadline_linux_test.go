//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func deadlineResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not retire")
		return nil
	}
}
func deadlineEntered(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not enter")
	}
}
func deadlineRead(c *SCTPConn) <-chan error {
	done := make(chan error, 1)
	go func() { _, e := c.Read(make([]byte, 32)); done <- e }()
	return done
}
func TestMutableDeadlineRealSCTP(t *testing.T) {
	client, server := lifecyclePair(t)
	// A persistent deadline applies before syscall admission and to future reads.
	if e := server.SetReadDeadline(time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	for j := 0; j < 2; j++ {
		if e := deadlineResult(t, deadlineRead(server)); !errors.Is(e, os.ErrDeadlineExceeded) {
			t.Fatal(e)
		}
	}
	if e := server.SetReadDeadline(time.Time{}); e != nil {
		t.Fatal(e)
	}
	done := deadlineRead(server)
	if e := server.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, done); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	// Read cleanup must not erase a concurrently installed persistent deadline.
	if e := deadlineResult(t, deadlineRead(server)); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	if e := server.SetReadDeadline(time.Time{}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.Write([]byte("ready")); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, deadlineRead(server)); e != nil {
		t.Fatal(e)
	}
	// Read and write directions remain independent.
	if e := server.SetReadDeadline(time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e := server.Write([]byte("other-direction")); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, deadlineRead(client)); e != nil {
		t.Fatal(e)
	}
}
func TestMutableDeadlineGateCancellationAndWake(t *testing.T) {
	_, server := lifecyclePair(t)
	ownerEntered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	owner := make(chan error, 1)
	go func() {
		owner <- server.io.pollContext(context.Background(), true, time.Time{}, func(uintptr) bool { close(ownerEntered); <-release; return true })
	}()
	deadlineEntered(t, ownerEntered)
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() {
		waiting <- server.io.pollContext(ctx, true, time.Time{}, func(uintptr) bool { t.Error("cancelled queued operation ran"); return true })
	}()
	cancel()
	if e := deadlineResult(t, waiting); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	go func() {
		waiting <- server.io.pollContext(context.Background(), true, time.Time{}, func(uintptr) bool { t.Error("expired queued operation ran"); return true })
	}()
	if e := server.SetReadDeadline(time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, waiting); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	// Already admitted progress wins even when public deadline expires meanwhile.
	once.Do(func() { close(release) })
	if e := deadlineResult(t, owner); e != nil {
		t.Fatal(e)
	}
}
func TestMutableDeadlineExtendClearAndOldHook(t *testing.T) {
	for _, clear := range []bool{false, true} {
		client, server := lifecyclePair(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		first := true
		go func() {
			done <- server.io.pollContext(ctx, true, time.Time{}, func(fd uintptr) bool {
				if first {
					first = false
					close(entered)
					<-release
				}
				_, _, _, _, e := syscall.Recvmsg(int(fd), make([]byte, 32), nil, 0)
				return e != syscall.EAGAIN && e != syscall.EINTR
			})
		}()
		deadlineEntered(t, entered)
		server.SetReadDeadline(time.Now().Add(-time.Second))
		next := time.Now().Add(time.Second)
		if clear {
			next = time.Time{}
		}
		if e := server.SetReadDeadline(next); e != nil {
			t.Fatal(e)
		}
		once.Do(func() { close(release) })
		if _, e := client.Write([]byte("ready")); e != nil {
			t.Fatal(e)
		}
		if e := deadlineResult(t, done); e != nil {
			t.Fatal(e)
		}
		cancel() // callback must have been removed/joined before the previous return
		if _, e := client.Write([]byte("successor")); e != nil {
			t.Fatal(e)
		}
		if e := deadlineResult(t, deadlineRead(server)); e != nil {
			t.Fatal("old callback poisoned successor", e)
		}
	}
}
func TestMutableDeadlineContextOwnerAndSocketBudget(t *testing.T) {
	client, server := lifecyclePair(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- server.io.pollContext(ctx, true, time.Time{}, func(fd uintptr) bool {
			once.Do(func() { close(entered) })
			_, _, _, _, e := syscall.Recvmsg(int(fd), make([]byte, 32), nil, 0)
			return e != syscall.EAGAIN && e != syscall.EINTR
		})
	}()
	deadlineEntered(t, entered)
	cancel()
	if e := deadlineResult(t, done); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := client.Write([]byte("alive")); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, deadlineRead(server)); e != nil {
		t.Fatal(e)
	}
	// Clearing public D cannot clear the original S budget.
	server.SetReadDeadline(time.Now().Add(time.Hour))
	done = make(chan error, 1)
	go func() {
		done <- server.io.pollContext(context.Background(), true, time.Now().Add(30*time.Millisecond), func(uintptr) bool { return false })
	}()
	server.SetReadDeadline(time.Time{})
	if e := deadlineResult(t, done); !errors.Is(e, ErrSocketTimeout) || !errors.Is(e, syscall.EAGAIN) {
		t.Fatal(e)
	}
	ctx2, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel2()
	if e := server.io.pollContext(ctx2, true, time.Time{}, func(uintptr) bool { t.Error("expired ctx ran"); return true }); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestMutableDeadlinePrecedenceAndAtomicPair(t *testing.T) {
	_, server := lifecyclePair(t)
	past := time.Now().Add(-time.Second)
	server.SetDeadline(past)
	server.io.deadlineMu.Lock()
	if !server.io.readDeadline.deadline.Equal(server.io.writeDeadline.deadline) {
		t.Error("split SetDeadline")
	}
	server.io.deadlineMu.Unlock()
	if e := server.io.pollContext(context.Background(), true, past, func(uintptr) bool { return true }); !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := server.io.pollContext(ctx, true, past, func(uintptr) bool { return true }); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	server.Close()
	if e := server.io.pollContext(ctx, true, past, func(uintptr) bool { return true }); !errors.Is(e, net.ErrClosed) {
		t.Fatal(e)
	}
	if e := server.SetDeadline(time.Time{}); !errors.Is(e, net.ErrClosed) {
		t.Fatal(e)
	}
}
func TestMutableDeadlineSuccessfulAdmissionWinsCancelClose(t *testing.T) {
	for _, closing := range []bool{false, true} {
		_, server := lifecyclePair(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		done := make(chan error, 1)
		go func() {
			done <- server.io.pollContext(ctx, false, time.Time{}, func(uintptr) bool { close(entered); <-release; return true })
		}()
		deadlineEntered(t, entered)
		cancel()
		closed := make(chan error, 1)
		if closing {
			go func() { closed <- server.Close() }()
			<-server.io.closing
		}
		once.Do(func() { close(release) })
		if e := deadlineResult(t, done); e != nil {
			t.Fatal(e)
		}
		if closing {
			deadlineResult(t, closed)
		}
	}
}
func TestMutableDeadlineInstallationFailureClosesOwner(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "not-pollable")
	if e != nil {
		t.Fatal(e)
	}
	s := newSocketIO(f)
	if e = s.setDeadlines(true, true, time.Now()); !errors.Is(e, os.ErrNoDeadline) {
		t.Fatal(e)
	}
	if e = s.pollContext(context.Background(), true, time.Time{}, func(uintptr) bool { t.Error("failed owner admitted syscall"); return true }); !errors.Is(e, net.ErrClosed) || !errors.Is(e, os.ErrNoDeadline) {
		t.Fatal(e)
	}
}

func TestMutableDeadlineWriteAndResetBothDirections(t *testing.T) {
	client, server := lifecyclePair(t)
	if e := client.SetDeadline(time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if n, e := client.Write([]byte("not-sent")); n != 0 || !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(n, e)
	}
	if e := client.SetDeadline(time.Time{}); e != nil {
		t.Fatal(e)
	}
	if n, e := client.Write([]byte("one-message")); n != 11 || e != nil {
		t.Fatal(n, e)
	}
	b := make([]byte, 32)
	server.SetReadDeadline(time.Now().Add(time.Second))
	if n, e := server.Read(b); e != nil || string(b[:n]) != "one-message" {
		t.Fatal(n, e)
	}
	if _, e := server.Write([]byte("response")); e != nil {
		t.Fatal(e)
	}
	if e := deadlineResult(t, deadlineRead(client)); e != nil {
		t.Fatal(e)
	}
}
func TestMutableDeadlineQueuedCloseAndContextDeadline(t *testing.T) {
	_, server := lifecyclePair(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	owner := make(chan error, 1)
	go func() {
		owner <- server.io.pollContext(context.Background(), true, time.Time{}, func(uintptr) bool { close(entered); <-release; return true })
	}()
	deadlineEntered(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	queued := make(chan error, 1)
	go func() {
		queued <- server.io.pollContext(ctx, true, time.Time{}, func(uintptr) bool { t.Error("expired queued call ran"); return true })
	}()
	if e := deadlineResult(t, queued); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	go func() {
		queued <- server.io.pollContext(context.Background(), true, time.Time{}, func(uintptr) bool { t.Error("closed queued call ran"); return true })
	}()
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	if e := deadlineResult(t, queued); !errors.Is(e, net.ErrClosed) {
		t.Fatal(e)
	}
	once.Do(func() { close(release) })
	if e := deadlineResult(t, owner); e != nil {
		t.Fatal(e)
	}
	deadlineResult(t, closed)
}
