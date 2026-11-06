//go:build linux && !386

package sctp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

func lifecycleListener(t *testing.T) *SCTPListener {
	t.Helper()
	addr, err := ResolveSCTPAddr("sctp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := ListenSCTP("sctp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func lifecyclePair(t *testing.T) (*SCTPConn, *SCTPConn) {
	t.Helper()
	ln := lifecycleListener(t)
	client, err := DialSCTP("sctp", nil, ln.Addr().(*SCTPAddr))
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

// lifecycleEndpoint establishes a one-to-many association without depending
// on a blocking CONNECTX3 surviving signals. Readiness and the association ID
// come from an actual peer message, even if connect returned EINPROGRESS.
func lifecycleEndpoint(t *testing.T) (*SCTPConn, *SCTPConn, int) {
	t.Helper()
	ln := lifecycleListener(t)
	listenRaw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(listenRaw, syscall.SO_RCVTIMEO, time.Second); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, syscall.IPPROTO_SCTP)
	if err != nil {
		t.Fatal(err)
	}
	c := NewSCTPConn(fd, nil)
	t.Cleanup(func() { c.Close() })
	var reportedID int
	err = c.withFD(func(fd int) error {
		addr := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}}
		if err := SCTPBind(fd, addr, SCTP_BINDX_ADD_ADDR); err != nil {
			return err
		}
		var err error
		reportedID, err = SCTPConnect(fd, ln.Addr().(*SCTPAddr))
		return err
	})
	immediate := err == nil
	if err != nil && err != syscall.EINPROGRESS && err != syscall.EINTR && err != syscall.EALREADY && err != syscall.EISCONN {
		t.Fatal(err)
	}
	peer, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	peerRaw, err := peer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(peerRaw, syscall.SO_RCVTIMEO, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	raw, err := c.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	n, info, err := c.SCTPRead(buf)
	if err != nil || string(buf[:n]) != "ready" || info == nil || info.AssocID == 0 {
		t.Fatalf("association readiness: n=%d info=%+v error=%v", n, info, err)
	}
	if immediate && reportedID != int(info.AssocID) {
		t.Fatalf("successful connect ID=%d metadata ID=%d", reportedID, info.AssocID)
	}
	if err := c.SubscribeEvents(0); err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 0); err != nil {
		t.Fatal(err)
	}
	return c, peer, int(info.AssocID)
}

func TestOneToManySetupUnderSignals(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid := syscall.Gettid()
	stop, done, ready := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var signalErr error
	signals := 0
	go func() {
		defer close(done)
		signalErr = syscall.Tgkill(syscall.Getpid(), tid, syscall.SIGURG)
		if signalErr == nil {
			signals++
		}
		close(ready)
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if signalErr = syscall.Tgkill(syscall.Getpid(), tid, syscall.SIGURG); signalErr != nil {
					return
				}
				signals++
			}
		}
	}()
	defer func() {
		close(stop)
		<-done
		if signalErr != nil || signals < 2 {
			t.Errorf("signal stimulus: count=%d error=%v", signals, signalErr)
		}
	}()
	<-ready
	for range 20 {
		c, peer, id := lifecycleEndpoint(t)
		peeled, err := c.PeelOff(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peeled.Write([]byte("signal-safe setup")); err != nil {
			peeled.Close()
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, err := peer.Read(buf)
		peeled.Close()
		c.Close()
		peer.Close()
		if err != nil || string(buf[:n]) != "signal-safe setup" {
			t.Fatalf("post-setup payload: n=%d error=%v", n, err)
		}
	}
}

func lifecycleFD(t *testing.T, raw syscall.RawConn) int {
	t.Helper()
	var fd int
	if err := raw.Control(func(value uintptr) { fd = int(value) }); err != nil {
		t.Fatal(err)
	}
	return fd
}

func fdFlags(fd int) (uintptr, error) {
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if errno != 0 {
		return 0, errno
	}
	return flags, nil
}

func TestListenerClosePreservesReusedFD(t *testing.T) {
	ln := lifecycleListener(t)
	raw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	oldFD := lifecycleFD(t, raw)
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if int(replacement.Fd()) != oldFD {
		t.Fatalf("expected fd %d to be reused, got %d", oldFD, replacement.Fd())
	}
	if err := ln.Close(); err == nil {
		t.Error("second Close should fail")
	}
	if _, err := fdFlags(int(replacement.Fd())); err != nil {
		t.Fatalf("repeated Close affected unrelated file: %v", err)
	}
	if ln.Addr() != nil {
		t.Error("closed listener returned an unrelated address")
	}
}

func TestListenerCloseUnblocksAllAccepts(t *testing.T) {
	for _, fromFile := range []bool{false, true} {
		name := "direct"
		if fromFile {
			name = "file"
		}
		t.Run(name, func(t *testing.T) {
			ln := lifecycleListener(t)
			if fromFile {
				raw, err := ln.SyscallConn()
				if err != nil {
					t.Fatal(err)
				}
				var copied *SCTPListener
				var fileErr error
				if err := raw.Control(func(fd uintptr) {
					dupFD, err := syscall.Dup(int(fd))
					if err != nil {
						fileErr = err
						return
					}
					file := os.NewFile(uintptr(dupFD), "listener-copy")
					defer file.Close()
					copied, fileErr = FileListener(file)
				}); err != nil {
					t.Fatal(err)
				}
				if fileErr != nil {
					t.Fatal(fileErr)
				}
				ln = copied
				t.Cleanup(func() { copied.Close() })
			}
			const workers = 4
			started := make(chan struct{}, workers)
			results := make(chan error, workers)
			for range workers {
				go func() {
					started <- struct{}{}
					c, err := ln.AcceptSCTP()
					if c != nil {
						c.Close()
					}
					results <- err
				}()
			}
			for range workers {
				<-started
			}
			// Exercise an accept already waiting in the poller, not only calls after Close.
			time.Sleep(20 * time.Millisecond)
			if err := ln.Close(); err != nil {
				t.Fatal(err)
			}
			for range workers {
				select {
				case err := <-results:
					if err == nil {
						t.Error("Accept succeeded after Close")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("Close did not unblock all accepts")
				}
			}
		})
	}
}

func TestRawConnRejectsClosedOwner(t *testing.T) {
	for _, listener := range []bool{false, true} {
		name := "connection"
		if listener {
			name = "listener"
		}
		t.Run(name, func(t *testing.T) {
			var raw syscall.RawConn
			var closeOwner func() error
			if listener {
				ln := lifecycleListener(t)
				raw, _ = ln.SyscallConn()
				closeOwner = ln.Close
			} else {
				c, _ := lifecyclePair(t)
				raw, _ = c.SyscallConn()
				closeOwner = c.Close
			}
			oldFD := lifecycleFD(t, raw)
			if err := closeOwner(); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open("/dev/null")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if int(file.Fd()) != oldFD {
				t.Fatalf("fd was not reused: %d != %d", file.Fd(), oldFD)
			}
			called := false
			if err := raw.Control(func(uintptr) { called = true }); err == nil {
				t.Error("Control on closed owner succeeded")
			}
			if err := raw.Read(func(uintptr) bool { called = true; return true }); err == nil {
				t.Error("Read on closed owner succeeded")
			}
			if err := raw.Write(func(uintptr) bool { called = true; return true }); err == nil {
				t.Error("Write on closed owner succeeded")
			}
			if called {
				t.Error("callback was called with a recycled fd")
			}
			if _, err := fdFlags(int(file.Fd())); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRawConnPinsFDDuringCallback(t *testing.T) {
	for _, listener := range []bool{false, true} {
		for _, operation := range []string{"Control", "Read", "Write"} {
			name := "connection/" + operation
			if listener {
				name = "listener/" + operation
			}
			t.Run(name, func(t *testing.T) {
				var raw syscall.RawConn
				var closeOwner func() error
				if listener {
					ln := lifecycleListener(t)
					raw, _ = ln.SyscallConn()
					closeOwner = ln.Close
				} else {
					c, _ := lifecyclePair(t)
					raw, _ = c.SyscallConn()
					closeOwner = c.Close
				}
				entered := make(chan struct{})
				release := make(chan struct{})
				controlDone := make(chan error, 1)
				var callbackErr error
				callback := func(fd uintptr) { close(entered); <-release; _, callbackErr = fdFlags(int(fd)) }
				go func() {
					switch operation {
					case "Control":
						controlDone <- raw.Control(callback)
					case "Read":
						controlDone <- raw.Read(func(fd uintptr) bool { callback(fd); return true })
					case "Write":
						controlDone <- raw.Write(func(fd uintptr) bool { callback(fd); return true })
					}
				}()
				<-entered
				closeStarted := make(chan struct{})
				closeDone := make(chan error, 1)
				go func() { close(closeStarted); closeDone <- closeOwner() }()
				<-closeStarted
				time.Sleep(10 * time.Millisecond)
				close(release)
				select {
				case err := <-controlDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("callback did not return")
				}
				if callbackErr != nil {
					t.Fatalf("fd closed while callback was running: %v", callbackErr)
				}
				select {
				case err := <-closeDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("Close did not return after callback")
				}
			})
		}
	}
}

func TestAcceptedSocketCloseOnExec(t *testing.T) {
	_, server := lifecyclePair(t)
	raw, err := server.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	flags, err := fdFlags(lifecycleFD(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatal("accepted socket is inherited across exec")
	}
}

func TestKernelStatusConstants(t *testing.T) {
	// Values come from Linux UAPI linux/sctp.h, not enum positions in Go.
	for _, check := range []struct {
		name      string
		got, want int
	}{
		{"CLOSED", int(SCTP_CLOSED), 1}, {"COOKIE_WAIT", int(SCTP_COOKIE_WAIT), 2}, {"COOKIE_ECHOED", int(SCTP_COOKIE_ECHOED), 3}, {"ESTABLISHED", int(SCTP_ESTABLISHED), 4},
		{"SHUTDOWN_PENDING", int(SCTP_SHUTDOWN_PENDING), 5}, {"SHUTDOWN_SENT", int(SCTP_SHUTDOWN_SENT), 6}, {"SHUTDOWN_RECEIVED", int(SCTP_SHUTDOWN_RECEIVED), 7}, {"SHUTDOWN_ACK_SENT", int(SCTP_SHUTDOWN_ACK_SENT), 8},
		{"INACTIVE", int(SCTP_INACTIVE), 0}, {"ACTIVE", int(SCTP_ACTIVE), 2}, {"UNCONFIRMED", int(SCTP_UNCONFIRMED), 3},
	} {
		if check.got != check.want {
			t.Errorf("%s=%d, want %d", check.name, check.got, check.want)
		}
	}
	client, _ := lifecyclePair(t)
	status, err := client.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SCTP_ESTABLISHED {
		t.Errorf("connected association state=%d", status.State)
	}
	if status.PrimaryPeerAddr.State != SCTP_ACTIVE {
		t.Errorf("active peer state=%d", status.PrimaryPeerAddr.State)
	}
}

func TestPeelOffOwnsUsableSocket(t *testing.T) {
	original, server, id := lifecycleEndpoint(t)
	peeled, err := original.PeelOff(id)
	if err != nil {
		t.Fatal(err)
	}
	if peeled.fd() <= 2 {
		t.Fatalf("PeelOff returned standard fd %d", peeled.fd())
	}
	t.Cleanup(func() { peeled.Close() })
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("peeled association")
	if n, err := peeled.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("peeled Write=%d,%v", n, err)
	}
	buf := make([]byte, 128)
	if n, err := server.Read(buf); err != nil || !bytes.Equal(buf[:n], payload) {
		t.Fatalf("peeled data=%q, err=%v", buf[:n], err)
	}
	if peeled.RemoteAddr() == nil {
		t.Fatal("peeled socket has no peer")
	}
}

func TestConcurrentWritesPreserveSendInfo(t *testing.T) {
	client, server := lifecyclePair(t)
	if err := server.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
		t.Fatal(err)
	}
	info := &SndRcvInfo{PPID: 0x12345678}
	const writers = 4
	const messages = 16
	var wg sync.WaitGroup
	t.Cleanup(func() { client.Close(); server.Close(); wg.Wait() })
	for writer := range writers {
		wg.Go(func() {
			for j := range messages {
				payload := fmt.Appendf(nil, "%d:%d", writer, j)
				if n, err := client.SCTPWrite(payload, info); err != nil || n != len(payload) {
					t.Errorf("SCTPWrite=%d,%v", n, err)
					return
				}
			}
		})
	}
	seen := make(map[string]bool)
	for range writers * messages {
		buf := make([]byte, 64)
		n, received, err := server.SCTPRead(buf)
		if err != nil {
			t.Fatal(err)
		}
		if received == nil || received.PPID != 0x12345678 {
			t.Fatalf("wrong received PPID: %+v", received)
		}
		if seen[string(buf[:n])] {
			t.Fatalf("duplicate message %q", buf[:n])
		}
		seen[string(buf[:n])] = true
	}
	wg.Wait()
	if info.PPID != 0x12345678 {
		t.Fatalf("caller's PPID changed: %#x", info.PPID)
	}
}

func TestRawConnReadWaitForReadiness(t *testing.T) {
	client, server := lifecyclePair(t)
	raw, err := server.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt := make(chan struct{}, 1)
	readDone := make(chan error, 1)
	buf := make([]byte, 32)
	var n int
	calls := 0
	go func() {
		readDone <- raw.Read(func(fd uintptr) bool {
			calls++
			var err error
			n, err = syscall.Read(int(fd), buf)
			if err == syscall.EAGAIN {
				select {
				case firstAttempt <- struct{}{}:
				default:
				}
				return false
			}
			return true
		})
	}()
	select {
	case <-firstAttempt:
	case <-time.After(2 * time.Second):
		t.Fatal("RawConn.Read did not attempt non-blocking read")
	}
	if _, err := client.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RawConn.Read did not wake")
	}
	if calls < 2 || string(buf[:n]) != "ready" {
		t.Fatalf("readiness callbacks=%d, data=%q", calls, buf[:n])
	}
}

func TestRawConnWriteWaitForReadiness(t *testing.T) {
	client, server := lifecyclePair(t)
	if err := client.SetWriteBuffer(4096); err != nil {
		t.Fatal(err)
	}
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	blocked := make(chan struct{}, 1)
	done := make(chan error, 1)
	payload := bytes.Repeat([]byte("x"), 1024)
	var writeErr error
	calls := 0
	go func() {
		done <- raw.Write(func(fd uintptr) bool {
			calls++
			if calls == 1 {
				for {
					_, writeErr = syscall.Write(int(fd), payload)
					if writeErr == syscall.EINTR {
						continue
					}
					if writeErr == syscall.EAGAIN {
						blocked <- struct{}{}
						return false
					}
					if writeErr != nil {
						return true
					}
				}
			}
			_, writeErr = syscall.Write(int(fd), []byte("ready"))
			return writeErr != syscall.EAGAIN
		})
	}()
	select {
	case <-blocked:
	case err := <-done:
		t.Fatalf("write did not reach backpressure: %v, %v", err, writeErr)
	case <-time.After(2 * time.Second):
		t.Fatal("write buffer did not fill")
	}
	if err := server.file.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2048)
	for {
		n, err := server.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) == "ready" {
			break
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RawConn.Write did not wake")
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if calls < 2 {
		t.Fatal("write callback was not retried after EAGAIN")
	}
}

func TestConnectionCloseUnblocksReads(t *testing.T) {
	_, server := lifecyclePair(t)
	done := make(chan error, 1)
	go func() { _, err := server.Read(make([]byte, 16)); done <- err }()
	time.Sleep(10 * time.Millisecond)
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked Read succeeded")
		}
		if err != io.EOF && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("unexpected read error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
}

func TestNewSCTPConnOwnsFDZero(t *testing.T) {
	// Run in a child process so replacing stdin cannot affect the test runner.
	if os.Getenv("SCTP_TEST_FD_ZERO") == "1" {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
		if err != nil {
			t.Fatal(err)
		}
		if fd != 0 {
			if err := syscall.Dup3(fd, 0, 0); err != nil {
				t.Fatal(err)
			}
			syscall.Close(fd)
		}
		c := NewSCTPConn(0, nil)
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := fdFlags(0); err != syscall.EBADF {
			t.Fatalf("fd 0 was not closed: %v", err)
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestNewSCTPConnOwnsFDZero$", "-test.timeout=10s")
	child.Env = append(os.Environ(), "SCTP_TEST_FD_ZERO=1")
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("fd zero child failed: %v\n%s", err, output)
	}
}
