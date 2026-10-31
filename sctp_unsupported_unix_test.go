//go:build (linux && 386) || (!linux && !windows)

package sctp

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

func TestUnsupportedOwnsSocket(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 132)
	if err != nil {
		t.Fatal(err)
	}
	c := NewSCTPConn(fd, nil)
	_, stateErr := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if stateErr == nil {
		syscall.Close(fd)
		t.Fatal("constructor retained fd after failed initialization")
	}
	if !errors.Is(stateErr, syscall.EBADF) {
		t.Fatalf("fd state=%v", stateErr)
	}
	// Deliberately reuse exactly the owned number and exercise concurrent Close.
	replacement, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replacement != fd {
		if err := syscall.Dup2(replacement, fd); err != nil {
			syscall.Close(replacement)
			t.Fatal(err)
		}
		syscall.Close(replacement)
	}
	defer syscall.Close(fd)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := c.Close(); !errors.Is(err, ErrUnsupported) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if _, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE); err != nil {
		t.Fatalf("Close affected reused descriptor: %v", err)
	}
	if n, err := c.Read(nil); n != 0 || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Read=%d,%v", n, err)
	}
	if raw, err := c.SyscallConn(); raw != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("raw=%v,%v", raw, err)
	}
}

func TestUnsupportedOwnsFDZero(t *testing.T) {
	if os.Getenv("SCTP_UNSUPPORTED_FD_ZERO") == "1" {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 132)
		if err != nil {
			t.Fatal(err)
		}
		if fd != 0 {
			if err := syscall.Dup2(fd, 0); err != nil {
				syscall.Close(fd)
				t.Fatal(err)
			}
			syscall.Close(fd)
		}
		c := NewSCTPConn(0, nil)
		if _, err := syscall.GetsockoptInt(0, syscall.SOL_SOCKET, syscall.SO_TYPE); !errors.Is(err, syscall.EBADF) {
			syscall.Close(0)
			t.Fatalf("fd zero retained: %v", err)
		}
		if !errors.Is(c.Close(), ErrUnsupported) {
			t.Fatal("missing support error")
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestUnsupportedOwnsFDZero$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "SCTP_UNSUPPORTED_FD_ZERO=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fd zero child: %v\n%s", err, out)
	}
}

func TestUnsupportedCleanupError(t *testing.T) {
	// This positive but invalid fd exercises a real close failure without
	// injecting a fake error or retrying a possibly reused descriptor.
	c := NewSCTPConn(1<<30, nil)
	if err := c.Close(); !errors.Is(err, ErrUnsupported) || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("cleanup error=%v", err)
	}
	if n, _, err := c.SCTPRead(nil); n != 0 || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("saved error=%d,%v", n, err)
	}
}
