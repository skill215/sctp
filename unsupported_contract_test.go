//go:build !linux || (linux && 386)

package sctp

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestUnsupportedBoundedSetup(t *testing.T) {
	cfg := SetupConfig{Limits: AddressLimits{16, 256, 4096}}
	if _, e := SetupRequirements(cfg); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e := NewDialer("sctp4", "", "127.0.0.1:9", cfg); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e := ListenContext(context.Background(), "sctp4", ":0", cfg); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	ln := &SCTPListener{}
	if _, e := ln.AcceptSCTPContext(context.Background()); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if _, e := ln.BoundAddr(); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if e := ln.SetDeadline(time.Time{}); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	if e := (&SCTPConn{}).SetSocketTimeouts(0, 0); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
}
