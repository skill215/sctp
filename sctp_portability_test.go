package sctp

import (
	"syscall"
	"testing"
)

var _ syscall.Conn = (*SCTPConn)(nil)
var _ syscall.Conn = (*SCTPListener)(nil)

func TestUnsupportedSentinelExported(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("missing unsupported sentinel")
	}
}
