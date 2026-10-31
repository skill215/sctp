package sctp

import (
	"errors"
	"runtime"
)

// ErrUnsupported reports unavailable SCTP socket operations on this target.
// It is exported on supported targets too, for portable error checks.
var ErrUnsupported = errors.New("SCTP is unsupported on " + runtime.GOOS + "/" + runtime.GOARCH)
