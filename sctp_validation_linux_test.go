//go:build linux && !386

package sctp

import (
	"errors"
	"fmt"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestSetInitMsgRejectsOutOfRange(t *testing.T) {
	c, _ := lifecyclePair(t)
	read := func() InitMsg {
		t.Helper()
		var value InitMsg
		size := uint32(unsafe.Sizeof(value))
		if _, _, err := c.getsockopt(SCTP_INITMSG, uintptr(unsafe.Pointer(&value)), &size); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for field := range 4 {
		for _, bad := range []int{-1, 65536, 65537, int(^uint(0) >> 1)} {
			t.Run(fmt.Sprintf("field%d/%d", field, bad), func(t *testing.T) {
				if err := c.SetInitMsg(4, 5, 6, 7); err != nil {
					t.Fatal(err)
				}
				before := read()
				args := [4]int{4, 5, 6, 7}
				args[field] = bad
				if err := c.SetInitMsg(args[0], args[1], args[2], args[3]); !errors.Is(err, syscall.EINVAL) {
					t.Errorf("invalid value returned %v", err)
				}
				if got := read(); got != before {
					t.Errorf("invalid request changed kernel options: before=%+v after=%+v", before, got)
				}
			})
		}
	}
	if err := c.SetInitMsg(65535, 65535, 65535, 65535); err != nil {
		t.Fatal(err)
	}
	before := read()
	if err := c.SetInitMsg(0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != before {
		t.Fatalf("zero/default settings changed options: before=%+v after=%+v", before, got)
	}
}

func TestAssociationIDRejectsTruncation(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("out-of-range int association IDs require 64 bits")
	}
	c, peer, id := lifecycleEndpoint(t)
	queries := []struct {
		name string
		call func(int) (*SCTPAddr, error)
	}{{"local", c.SCTPLocalAddr}, {"remote", c.SCTPRemoteAddr}}
	for _, q := range queries {
		if _, err := q.call(id); err != nil {
			t.Fatal(err)
		}
		if _, err := q.call(2147483647); err == nil {
			t.Fatal("nonexistent in-range association unexpectedly exists")
		}
	}
	for _, value := range []int64{int64(id) + 1<<32, int64(id) - 1<<32, 1<<63 - 1, -1 << 63} {
		bad := int(value)
		for _, q := range queries {
			if addr, err := q.call(bad); !errors.Is(err, syscall.EINVAL) || addr != nil {
				t.Errorf("%s(%d)=%v,%v", q.name, bad, addr, err)
			}
		}
		peeled, err := c.PeelOff(bad)
		if peeled != nil {
			status, statusErr := peeled.GetStatus()
			peeled.Close()
			t.Fatalf("invalid ID %d peeled live association %d: status=%+v error=%v", bad, id, status, statusErr)
		}
		if !errors.Is(err, syscall.EINVAL) {
			t.Errorf("PeelOff(%d) error=%v", bad, err)
		}
	}
	// Rejection must leave the live association in place and usable.
	peeled, err := c.PeelOff(id)
	if err != nil {
		t.Fatal(err)
	}
	defer peeled.Close()
	raw, err := peer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := peeled.Write([]byte("still live")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if n, err := peer.Read(buf); err != nil || string(buf[:n]) != "still live" {
		t.Fatalf("valid association after rejected IDs: n=%d error=%v", n, err)
	}
}
