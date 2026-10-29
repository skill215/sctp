//go:build linux && !386
// +build linux,!386

package sctp

import (
	"errors"
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"
)

const testSocketTimeout = 40 * time.Millisecond

func setSocketTimeout(raw syscall.RawConn, option int, timeout time.Duration) error {
	var optionErr error
	value := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := raw.Control(func(fd uintptr) {
		optionErr = syscall.SetsockoptTimeval(int(fd), syscall.SOL_SOCKET, option, &value)
	}); err != nil {
		return err
	}
	return optionErr
}

func socketTimeoutConfig(option int, timeout time.Duration) SocketConfig {
	return SocketConfig{Control: func(_, _ string, raw syscall.RawConn) error { return setSocketTimeout(raw, option, timeout) }}
}

func timeoutPair(t *testing.T, option int) (*SCTPConn, *SCTPConn) {
	t.Helper()
	ln := lifecycleListener(t)
	cfg := socketTimeoutConfig(option, testSocketTimeout)
	client, err := cfg.Dial("sctp", nil, ln.Addr().(*SCTPAddr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return client, server
}

func awaitSocketTimeout(t *testing.T, done <-chan error, cancel func() error, start time.Time) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, syscall.EAGAIN) {
			t.Fatalf("expected socket timeout (EAGAIN), got %v", err)
		}
		if elapsed := time.Since(start); elapsed < testSocketTimeout/2 {
			t.Fatalf("timeout returned too early: %v", elapsed)
		}
	case <-time.After(time.Second):
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("operation did not stop after Close")
		}
		t.Fatal("configured socket timeout was ignored")
	}
}

func TestReadSocketTimeout(t *testing.T) {
	client, _ := timeoutPair(t, syscall.SO_RCVTIMEO)
	done := make(chan error, 1)
	start := time.Now()
	go func() { _, err := client.Read(make([]byte, 16)); done <- err }()
	awaitSocketTimeout(t, done, client.Close, start)
}

func TestWriteSocketTimeout(t *testing.T) {
	client, server := timeoutPair(t, syscall.SO_SNDTIMEO)
	if err := client.SetWriteBuffer(4096); err != nil {
		t.Fatal(err)
	}
	if err := server.SetReadBuffer(4096); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		for i := 0; i < 4096; i++ {
			if _, err := client.Write(make([]byte, 1024)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	awaitSocketTimeout(t, done, client.Close, start)
}

func TestAcceptSocketTimeout(t *testing.T) {
	addr, err := ResolveSCTPAddr("sctp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := socketTimeoutConfig(syscall.SO_RCVTIMEO, testSocketTimeout)
	ln, err := cfg.Listen("sctp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		conn, err := ln.AcceptSCTP()
		if conn != nil {
			conn.Close()
		}
		done <- err
	}()
	awaitSocketTimeout(t, done, ln.Close, start)
	// A timed-out accept must not leave a permanent deadline behind.
	client, err := DialSCTP("sctp", nil, ln.Addr().(*SCTPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	accepted, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	accepted.Close()
}

func TestConnectSocketTimeout(t *testing.T) {
	t.Run("plain", func(t *testing.T) { checkConnectSocketTimeout(t, false) })
	t.Run("interrupted", func(t *testing.T) { checkConnectSocketTimeout(t, true) })
}

func checkConnectSocketTimeout(t *testing.T, interrupted bool) {
	t.Helper()
	// Drop only this listener's incoming packets. This makes a loopback
	// handshake stall without firewall changes, privileges, or external hosts.
	drop := SocketConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		if err := raw.Control(func(fd uintptr) {
			optionErr = syscall.AttachLsf(int(fd), []syscall.SockFilter{{Code: syscall.BPF_RET | syscall.BPF_K, K: 0}})
		}); err != nil {
			return err
		}
		return optionErr
	}}
	addr, _ := ResolveSCTPAddr("sctp", "127.0.0.1:0")
	ln, err := drop.Listen("sctp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	cfg := socketTimeoutConfig(syscall.SO_SNDTIMEO, testSocketTimeout)
	configure := cfg.Control
	var pendingRaw syscall.RawConn
	cfg.Control = func(network, address string, raw syscall.RawConn) error {
		pendingRaw = raw
		return configure(network, address, raw)
	}
	// Bound the protocol retry time as a backstop for a broken poll timeout.
	cfg.InitMsg = InitMsg{MaxAttempts: 1, MaxInitTimeout: 100}
	if interrupted {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid := syscall.Gettid()
		stop, stopped, ready := make(chan struct{}), make(chan struct{}), make(chan struct{})
		signals := 0
		var signalErr error
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			signalErr = syscall.Tgkill(syscall.Getpid(), tid, syscall.SIGURG)
			if signalErr == nil {
				signals++
			}
			close(ready)
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if err := syscall.Tgkill(syscall.Getpid(), tid, syscall.SIGURG); err != nil {
						signalErr = err
						return
					}
					signals++
				}
			}
		}()
		defer func() {
			close(stop)
			<-stopped
			if signalErr != nil {
				t.Error(signalErr)
			}
			if signals < 2 {
				t.Errorf("only %d interrupt signals delivered", signals)
			}
		}()
		<-ready
	}
	start := time.Now()
	conn, err := cfg.Dial("sctp", nil, ln.Addr().(*SCTPAddr))
	if conn != nil {
		conn.Close()
	}
	if !errors.Is(err, syscall.EINPROGRESS) {
		t.Fatalf("expected connect socket timeout (EINPROGRESS), got %v after %v", err, time.Since(start))
	}
	if elapsed := time.Since(start); elapsed < testSocketTimeout/2 || elapsed > time.Second {
		t.Fatalf("connect timeout elapsed %v", elapsed)
	}
	called := false
	if err := pendingRaw.Control(func(uintptr) { called = true }); err == nil || called {
		t.Fatal("timed-out dial left its socket usable")
	}
}

func TestReadSocketTimeoutCanBeCleared(t *testing.T) {
	client, server := timeoutPair(t, syscall.SO_RCVTIMEO)
	done := make(chan error, 1)
	start := time.Now()
	go func() { _, err := client.Read(make([]byte, 16)); done <- err }()
	awaitSocketTimeout(t, done, client.Close, start)
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 0); err != nil {
		t.Fatal(err)
	}
	type result struct {
		data string
		err  error
	}
	received := make(chan result, 1)
	go func() { buf := make([]byte, 16); n, err := client.Read(buf); received <- result{string(buf[:n]), err} }()
	select {
	case r := <-received:
		t.Fatalf("cleared timeout returned early: %+v", r)
	case <-time.After(2 * testSocketTimeout):
	}
	if _, err := server.Write([]byte("resumed")); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-received:
		if r.err != nil || r.data != "resumed" {
			t.Fatalf("read after clearing timeout: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not resume")
	}
}

func TestQueuedReadHasIndependentTimeout(t *testing.T) {
	client, _ := lifecyclePair(t)
	firstDone := make(chan struct{})
	go func() { defer close(firstDone); client.Read(make([]byte, 16)) }()
	t.Cleanup(func() { client.Close(); <-firstDone })
	// Let the first reader enter an unlimited wait before changing the option.
	time.Sleep(20 * time.Millisecond)
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, testSocketTimeout); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() { _, err := client.Read(make([]byte, 16)); done <- err }()
	awaitSocketTimeout(t, done, client.Close, start)
	select {
	case <-firstDone:
		t.Fatal("queued timeout changed the active reader's deadline")
	default:
	}
}

func TestCloseUnblocksQueuedReads(t *testing.T) {
	client, _ := lifecyclePair(t)
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := client.Read(make([]byte, 16)); done <- err }()
	}
	time.Sleep(20 * time.Millisecond)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, net.ErrClosed) {
				t.Fatalf("closed read returned %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Close did not unblock queued read")
		}
	}
}

func TestWriteSocketTimeoutCanBeCleared(t *testing.T) {
	client, server := timeoutPair(t, syscall.SO_SNDTIMEO)
	if err := client.SetWriteBuffer(4096); err != nil {
		t.Fatal(err)
	}
	if err := server.SetReadBuffer(4096); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		for i := 0; i < 4096; i++ {
			if _, err := client.Write(make([]byte, 1024)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	awaitSocketTimeout(t, done, client.Close, start)
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_SNDTIMEO, 0); err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := server.Read(buf)
			if err != nil {
				drained <- err
				return
			}
			if string(buf[:n]) == "resumed" {
				drained <- nil
				return
			}
		}
	}()
	defer func() { client.Close(); server.Close() }()
	if _, err := client.Write([]byte("resumed")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("write did not resume after clearing timeout")
	}
}

func TestConnectTimeoutPreservesRefusal(t *testing.T) {
	ln := lifecycleListener(t)
	addr := ln.Addr().(*SCTPAddr)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := socketTimeoutConfig(syscall.SO_SNDTIMEO, time.Second)
	conn, err := cfg.Dial("sctp", nil, addr)
	if conn != nil {
		conn.Close()
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("expected connection refused, got %v", err)
	}
}
