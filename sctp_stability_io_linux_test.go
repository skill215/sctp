//go:build linux && !386

package sctp

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"
)

func stabilityAbort(t *testing.T, peer *SCTPConn) {
	t.Helper()
	if err := peer.withFD(func(fd int) error {
		return syscall.SetsockoptLinger(fd, syscall.SOL_SOCKET, syscall.SO_LINGER, &syscall.Linger{Onoff: 1})
	}); err != nil {
		t.Fatal(err)
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}
}

// #4: both queued data and queued notifications must survive empty reads.
func TestStabilityEmptyReadsPreserveInput(t *testing.T) {
	client, peer := lifecyclePair(t)
	wrapped := NewSCTPSndRcvInfoWrappedConn(client)
	if err := client.SubscribeEvents(SCTP_EVENT_DATA_IO | SCTP_EVENT_SENDER_DRY); err != nil {
		t.Fatal(err)
	}
	marker := []byte("complete marker")
	if _, err := peer.Write(marker); err != nil {
		t.Fatal(err)
	}
	for _, b := range [][]byte{nil, {}, make([]byte, 0, 64)} {
		if n, info, err := client.SCTPRead(b); n != 0 || info != nil || err != nil {
			t.Fatalf("empty SCTPRead=%d,%v,%v", n, info, err)
		}
		if n, err := client.Read(b); n != 0 || err != nil {
			t.Fatalf("empty Read=%d,%v", n, err)
		}
	}
	for _, size := range []int{0, 31, 32} {
		b := bytes.Repeat([]byte{0xa5}, size)
		before := bytes.Clone(b)
		if n, err := wrapped.Read(b); n != 0 || !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("wrapped size=%d: %d,%v", size, n, err)
		}
		if !bytes.Equal(b, before) {
			t.Fatal("short read changed buffer")
		}
	}
	// SENDER_DRY was queued by subscription and carries no metadata (#9).
	b := bytes.Repeat([]byte{0xa5}, 128)
	n, err := wrapped.Read(b)
	if err != nil || n != 44 || nativeEndian.Uint16(b[32:34]) != uint16(SCTP_SENDER_DRY_EVENT) {
		t.Fatalf("notification=%d,%v %x", n, err, b[:max(0, n)])
	}
	if !bytes.Equal(b[:32], make([]byte, 32)) {
		t.Fatalf("stale metadata header: %x", b[:32])
	}
	n, err = wrapped.Read(b)
	if err != nil || !bytes.Equal(b[32:n], marker) {
		t.Fatalf("payload=%d,%v %q", n, err, b[32:max(32, n)])
	}
}

func TestStabilityEmptyReadObjectErrors(t *testing.T) {
	var absent *SCTPConn
	failed := NewSCTPConn(-1, nil)
	client, _ := lifecyclePair(t)
	client.Close()
	for _, c := range []*SCTPConn{absent, failed, client} {
		if n, _, err := c.SCTPRead(nil); n != 0 || err == nil {
			t.Fatalf("invalid empty read=%d,%v", n, err)
		}
	}
}

// #12: real resets must not violate io.Reader's count contract.
func TestStabilityResetReadCounts(t *testing.T) {
	for _, mode := range []string{"sctp", "wrapped", "readall"} {
		t.Run(mode, func(t *testing.T) {
			c, p := lifecyclePair(t)
			w := NewSCTPSndRcvInfoWrappedConn(c)
			raw, _ := c.SyscallConn()
			if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 200*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			stabilityAbort(t, p)
			var n int
			var err error
			switch mode {
			case "sctp":
				n, _, err = c.SCTPRead(make([]byte, 128))
			case "wrapped":
				n, err = w.Read(make([]byte, 128))
			case "readall":
				var b []byte
				b, err = io.ReadAll(w)
				n = len(b)
			}
			if n != 0 || !errors.Is(err, syscall.ECONNRESET) {
				t.Fatalf("reset read=%d,%v", n, err)
			}
		})
	}
}

func TestStabilityWrappedTerminalCounts(t *testing.T) {
	for _, mode := range []string{"eof", "timeout", "close"} {
		t.Run(mode, func(t *testing.T) {
			c, p := lifecyclePair(t)
			w := NewSCTPSndRcvInfoWrappedConn(c)
			switch mode {
			case "eof":
				p.Close()
			case "timeout":
				raw, _ := c.SyscallConn()
				if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 25*time.Millisecond); err != nil {
					t.Fatal(err)
				}
			case "close":
				c.Close()
			}
			n, err := w.Read(make([]byte, 128))
			if n != 0 || err == nil {
				t.Fatalf("terminal read=%d,%v", n, err)
			}
			if mode == "eof" && !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			if mode == "timeout" && !errors.Is(err, syscall.EAGAIN) {
				t.Fatal(err)
			}
		})
	}
}

// #8: adding DATA_IO must retain existing subscriptions and actual delivery.
func TestStabilityWrappingPreservesEvents(t *testing.T) {
	c, p := lifecyclePair(t)
	delivered := make(chan struct{}, 1)
	c.notificationHandler = func(b []byte) error {
		if nativeEndian.Uint16(b[:2]) == uint16(SCTP_SHUTDOWN_EVENT) {
			delivered <- struct{}{}
		}
		// Reentrant configuration must not deadlock with the wrapper's lock.
		_, err := c.SubscribedEvents()
		return err
	}
	if err := c.SubscribeEvents(SCTP_EVENT_SHUTDOWN); err != nil {
		t.Fatal(err)
	}
	w := NewSCTPSndRcvInfoWrappedConn(c)
	NewSCTPSndRcvInfoWrappedConn(c)
	flags, err := c.SubscribedEvents()
	if err != nil || flags != SCTP_EVENT_SHUTDOWN|SCTP_EVENT_DATA_IO {
		t.Fatalf("events=%x,%v", flags, err)
	}
	p.Close()
	if n, err := w.Read(make([]byte, 128)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("shutdown=%d,%v", n, err)
	}
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("shutdown handler lost")
	}
}

func TestStabilityConcurrentEventConfiguration(t *testing.T) {
	c, _ := lifecyclePair(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for j := 0; j < 10; j++ {
				if err := c.SubscribeEvents(SCTP_EVENT_SHUTDOWN); err != nil {
					t.Error(err)
				}
				NewSCTPSndRcvInfoWrappedConn(c)
				if _, err := c.SubscribedEvents(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	flags, err := c.SubscribedEvents()
	if err != nil || flags != SCTP_EVENT_SHUTDOWN|SCTP_EVENT_DATA_IO {
		t.Fatalf("final events=%x,%v", flags, err)
	}
}

func TestStabilityWrapperSetupFailure(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[1])
	c := NewSCTPConn(fds[0], nil)
	w := NewSCTPSndRcvInfoWrappedConn(c)
	// Unix sockets support I/O but cannot enable SCTP metadata. Neither method
	// may enter I/O after setup failure, even when a complete frame is supplied.
	done := make(chan error, 1)
	go func() { _, err := w.Read(make([]byte, 128)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("missing setup error")
		}
	case <-time.After(time.Second):
		c.Close()
		<-done
		t.Fatal("setup failure entered read")
	}
	if n, err := w.Write(make([]byte, 33)); n != 0 || err == nil {
		t.Fatalf("setup write=%d,%v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := syscall.GetsockoptInt(fds[0], syscall.SOL_SOCKET, syscall.SO_TYPE); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("setup Close did not release fd: %v", err)
	}
	var absent *SCTPConn
	if n, err := NewSCTPSndRcvInfoWrappedConn(absent).Read(make([]byte, 128)); n != 0 || err == nil {
		t.Fatalf("nil wrapper=%d,%v", n, err)
	}
}

func TestStabilityWrappedPartialData(t *testing.T) {
	for _, info := range []*SndRcvInfo{nil, {PPID: 42, Stream: 1}} {
		b := bytes.Repeat([]byte{0xa5}, 64)
		copy(b[32:], "data")
		n, err := wrappedReadResult(b, 4, info, syscall.EINVAL)
		if n != 36 || !errors.Is(err, syscall.EINVAL) || string(b[32:n]) != "data" {
			t.Fatalf("partial frame=%d,%v", n, err)
		}
		var got SndRcvInfo
		if err := decodeNative(b[:32], &got); err != nil {
			t.Fatal(err)
		}
		if info == nil && got != (SndRcvInfo{}) || info != nil && got != *info {
			t.Fatalf("header=%+v", got)
		}
	}
}

func TestStabilityAcceptErrorNil(t *testing.T) {
	ln := lifecycleListener(t)
	ln.Close()
	c, err := ln.Accept()
	if c != nil || err == nil {
		t.Fatalf("Accept=%v,%v", c, err)
	}
}

func TestStabilityNotificationHeader(t *testing.T) {
	c, p := lifecyclePair(t)
	w := NewSCTPSndRcvInfoWrappedConn(c)
	if _, err := p.SCTPWrite([]byte("metadata"), &SndRcvInfo{Stream: 1, PPID: 42}); err != nil {
		t.Fatal(err)
	}
	b := bytes.Repeat([]byte{0xa5}, 128)
	n, err := w.Read(b)
	var info SndRcvInfo
	if err != nil || n != 40 {
		t.Fatalf("metadata control=%d,%v", n, err)
	}
	if err := decodeNative(b[:32], &info); err != nil || info.PPID != 42 || info.Stream != 1 {
		t.Fatalf("metadata=%+v,%v", info, err)
	}
	for range 2 {
		if err := c.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
			t.Fatal(err)
		}
		if err := c.SubscribeEvents(SCTP_EVENT_DATA_IO | SCTP_EVENT_SENDER_DRY); err != nil {
			t.Fatal(err)
		}
		n, err = w.Read(b)
		if err != nil || n != 44 || nativeEndian.Uint16(b[32:34]) != uint16(SCTP_SENDER_DRY_EVENT) || !bytes.Equal(b[:32], make([]byte, 32)) {
			t.Fatalf("reused notification frame=%d,%v %x", n, err, b[:44])
		}
	}
}
