//go:build linux && !386
// +build linux,!386

package sctp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
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
			for i := 0; i < workers; i++ {
				go func() {
					started <- struct{}{}
					c, err := ln.AcceptSCTP()
					if c != nil {
						c.Close()
					}
					results <- err
				}()
			}
			for i := 0; i < workers; i++ {
				<-started
			}
			// Exercise an accept already waiting in the poller, not only calls after Close.
			time.Sleep(20 * time.Millisecond)
			if err := ln.Close(); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < workers; i++ {
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
	ln := lifecycleListener(t)
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
	if err != nil {
		t.Fatal(err)
	}
	// Start the association with a blocking socket, then transfer ownership.
	id, err := SCTPConnect(fd, ln.Addr().(*SCTPAddr))
	if err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	original := NewSCTPConn(fd, nil)
	t.Cleanup(func() { original.Close() })
	server, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
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
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for j := 0; j < messages; j++ {
				payload := []byte(fmt.Sprintf("%d:%d", writer, j))
				if n, err := client.SCTPWrite(payload, info); err != nil || n != len(payload) {
					t.Errorf("SCTPWrite=%d,%v", n, err)
					return
				}
			}
		}(i)
	}
	seen := make(map[string]bool)
	for i := 0; i < writers*messages; i++ {
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
