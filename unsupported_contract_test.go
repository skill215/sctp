//go:build !linux || (linux && 386)

package sctp

import (
	"errors"
	"testing"
)

func TestUnsupportedParameterValidation(t *testing.T) {
	c := NewSCTPConn(-1, nil)
	for _, value := range []int{-1, 0, 65536} {
		if err := c.SetInitMsg(value, value, value, value); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("SetInitMsg(%d)=%v", value, err)
		}
		if got, err := c.PeelOff(value); got != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatalf("PeelOff(%d)=%v,%v", value, got, err)
		}
	}
}
