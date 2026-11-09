//go:build !linux || (linux && 386)

package sctp

import (
	"syscall"
	"time"
)

func (c *SCTPConn) setPublicDeadline(read, write bool, t time.Time) error { return syscall.EOPNOTSUPP }
