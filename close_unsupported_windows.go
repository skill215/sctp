package sctp

import "syscall"

func closeUnsupportedSocket(fd int) error { return syscall.Closesocket(syscall.Handle(fd)) }
