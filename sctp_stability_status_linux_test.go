//go:build linux && !386

package sctp

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// This probe is also run before enabling the liveness change on another
// native kernel. EINVAL is meaningful only with this exact valid ABI/scope.
func TestStabilityAssociationStatusProbe(t *testing.T) {
	if unsafe.Sizeof(Status{}) != 176 {
		t.Fatalf("status ABI size=%d", unsafe.Sizeof(Status{}))
	}
	c, p := lifecyclePair(t)
	if status, err := c.GetStatus(); err != nil || status.State != SCTP_ESTABLISHED {
		t.Fatalf("live status=%+v,%v", status, err)
	}
	stabilityAbort(t, p)
	if _, err := c.Write([]byte("consume reset")); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("reset trigger=%v", err)
	}
	if _, err := c.GetStatus(); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("terminated status=%v", err)
	}
	var recvErr error
	if err := c.withFD(func(fd int) error { _, _, _, _, recvErr = syscall.Recvmsg(fd, make([]byte, 64), nil, 0); return nil }); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(recvErr, syscall.EAGAIN) {
		t.Fatalf("terminated nonblocking recv=%v", recvErr)
	}
	ln := lifecycleListener(t)
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 132)
	if err != nil {
		t.Fatal(err)
	}
	id, err := SCTPConnect(fd, ln.Addr().(*SCTPAddr))
	if err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	endpoint := NewSCTPConn(fd, nil)
	defer endpoint.Close()
	server, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := endpoint.GetStatus(); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("endpoint status zero=%v", err)
	}
	peeled, err := endpoint.PeelOff(id)
	if err != nil {
		t.Fatal(err)
	}
	defer peeled.Close()
	if err := peeled.withFD(func(fd int) error {
		kind, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE)
		if err == nil && kind != syscall.SOCK_SEQPACKET {
			t.Errorf("peeled type=%d", kind)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if status, err := peeled.GetStatus(); err != nil || status.State != SCTP_ESTABLISHED {
		t.Fatalf("peeled status=%+v,%v", status, err)
	}
}
