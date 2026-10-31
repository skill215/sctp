//go:build linux && !386

package sctp

import (
	"syscall"
	"testing"
)

// Non-SCTP ABI test: a real zero-byte datagram must still transfer SCM_RIGHTS.
// This runs under QEMU on 32-bit and big-endian architectures too.
func TestSendmsgZeroAncillaryABI(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[0])
	defer syscall.Close(fds[1])
	if n, err := sendmsgZero(fds[0], syscall.UnixRights(fds[0])); n != 0 || err != nil {
		t.Fatalf("send=%d,%v", n, err)
	}
	b := make([]byte, 8)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, on, flags, _, err := syscall.Recvmsg(fds[1], b, oob, syscall.MSG_DONTWAIT)
	if err != nil || n != 0 || flags&syscall.MSG_CTRUNC != 0 {
		t.Fatalf("recv=%d,%d,%x,%v", n, on, flags, err)
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:on])
	if err != nil || len(msgs) != 1 {
		t.Fatalf("cmsgs=%v,%v", msgs, err)
	}
	rights, err := syscall.ParseUnixRights(&msgs[0])
	if err != nil || len(rights) != 1 {
		t.Fatalf("rights=%v,%v", rights, err)
	}
	defer syscall.Close(rights[0])
	if _, err := syscall.GetsockoptInt(rights[0], syscall.SOL_SOCKET, syscall.SO_TYPE); err != nil {
		t.Fatalf("invalid transferred descriptor: %v", err)
	}
}
