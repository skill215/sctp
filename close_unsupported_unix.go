//go:build (linux && 386) || (!linux && !windows)

package sctp

import "syscall"

func closeUnsupportedSocket(fd int) error { return syscall.Close(fd) }
