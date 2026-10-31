//go:build linux && !386

package sctp

import (
	"runtime"
	"syscall"
	"unsafe"
)

// sendmsgZero preserves kernel/default control semantics without SendmsgN's
// synthetic ancillary-data byte on non-datagram sockets. The caller owns the
// socket's write gate, retry loop and timeout budget.
func sendmsgZero(fd int, oob []byte) (int, error) {
	var msg syscall.Msghdr
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.SetControllen(len(oob))
	}
	n, _, errno := syscall.Syscall(syscall.SYS_SENDMSG, uintptr(fd), uintptr(unsafe.Pointer(&msg)), 0)
	runtime.KeepAlive(oob)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}
