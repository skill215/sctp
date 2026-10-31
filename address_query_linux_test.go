//go:build linux && !386

package sctp

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestAddressQueryGrowth(t *testing.T) {
	raw := (&SCTPAddr{Port: 42, IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}, {IP: net.IPv6loopback}}}).ToRawSockAddrBuf()
	for _, opt := range []int{SCTP_GET_LOCAL_ADDRS, SCTP_GET_PEER_ADDRS} {
		calls := 0
		addr, err := querySCTPAddrs(7, opt, func(b []byte, size *uint32) error {
			calls++
			if nativeEndian.Uint32(b[:4]) != 7 || nativeEndian.Uint32(b[4:8]) != 0 || int(*size) != len(b) {
				t.Fatal("query state not reinitialized")
			}
			if calls == 1 {
				nativeEndian.PutUint32(b[4:8], 99)
				*size = 1
				return syscall.ENOMEM
			}
			if len(b) != 8+8192 {
				t.Fatalf("growth=%d", len(b))
			}
			nativeEndian.PutUint32(b[4:8], 2)
			copy(b[8:], raw)
			*size = uint32(len(raw))
			if opt == SCTP_GET_PEER_ADDRS {
				*size += 8
			}
			return nil
		})
		if err != nil || calls != 2 || addr.Port != 42 || len(addr.IPAddrs) != 2 || !addr.IPAddrs[1].IP.Equal(net.IPv6loopback) {
			t.Fatalf("grown result=%v,%v calls=%d", addr, err, calls)
		}
	}
}

func TestAddressQueryBudgetAndErrors(t *testing.T) {
	calls := 0
	a, err := querySCTPAddrs(0, SCTP_GET_LOCAL_ADDRS, func(b []byte, size *uint32) error {
		calls++
		if len(b) != 8+(4096<<(calls-1)) {
			t.Fatal("wrong growth")
		}
		return syscall.ENOMEM
	})
	if a != nil || calls != 9 || !errors.Is(err, syscall.ENOMEM) || !strings.Contains(err.Error(), "1048576") {
		t.Fatalf("budget=%v,%v calls=%d", a, err, calls)
	}
	calls = 0
	a, err = querySCTPAddrs(0, SCTP_GET_LOCAL_ADDRS, func(b []byte, size *uint32) error { calls++; return syscall.EPERM })
	if a != nil || calls != 1 || !errors.Is(err, syscall.EPERM) {
		t.Fatalf("non-memory error=%v,%v calls=%d", a, err, calls)
	}
	for _, mode := range []string{"too-large", "peer-short", "count", "truncated", "family"} {
		t.Run(mode, func(t *testing.T) {
			a, err := querySCTPAddrs(0, SCTP_GET_PEER_ADDRS, func(b []byte, size *uint32) error {
				switch mode {
				case "too-large":
					*size = uint32(len(b) + 1)
				case "peer-short":
					*size = 7
				case "count":
					nativeEndian.PutUint32(b[4:8], ^uint32(0))
					*size = 8
				case "truncated":
					nativeEndian.PutUint32(b[4:8], 1)
					*size = 9
				case "family":
					nativeEndian.PutUint32(b[4:8], 1)
					*size = 24
					nativeEndian.PutUint16(b[8:10], 0xffff)
				}
				return nil
			})
			if a != nil || err == nil {
				t.Fatalf("accepted malformed result=%v,%v", a, err)
			}
		})
	}
}
