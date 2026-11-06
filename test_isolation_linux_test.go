//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestListenersAvoidReservedLegacyPorts(t *testing.T) {
	for _, port := range []int{54321, 7777} {
		fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { syscall.Close(fd) })
		if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}, Port: port}); err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				// Already unavailable is also a valid isolation condition.
				t.Logf("legacy port %d is already in use", port)
				continue
			}
			t.Fatal(err)
		}
		if err := syscall.Listen(fd, 5); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	// The child pattern excludes this parent test to prevent recursion.
	cmd := exec.CommandContext(ctx, exe,
		"-test.run=^Test(NotificationHandlerAssignmentOnDialing|NotificationHandlerAssignmentOnListening|SyscallConn|SCTPListenerNameFromFd|SCTPListenerName)$",
		"-test.v", "-test.timeout=15s")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tests depended on legacy ports: %v\n%s", err, output)
	}
}
