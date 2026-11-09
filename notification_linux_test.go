//go:build linux && !386

package sctp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"reflect"
	"syscall"
	"testing"
)

func notificationFixture(kind SCTPNotificationType, size int) []byte {
	p := make([]byte, size)
	nativeEndian.PutUint16(p, uint16(kind))
	nativeEndian.PutUint32(p[4:], uint32(size))
	return p
}
func TestNotificationFiveLayouts(t *testing.T) {
	t.Run("association", func(t *testing.T) {
		p := notificationFixture(SCTP_ASSOC_CHANGE, 23)
		nativeEndian.PutUint16(p[2:], 0xff)
		nativeEndian.PutUint16(p[8:], 0xfffe)
		nativeEndian.PutUint16(p[10:], 19)
		nativeEndian.PutUint16(p[12:], 3)
		nativeEndian.PutUint16(p[14:], 5)
		nativeEndian.PutUint32(p[16:], 0xfffffffe)
		copy(p[20:], "abc")
		n, e := ParseNotification(p)
		if e != nil || n.Kind != NotificationAssociationChange || n.Header.Flags != 255 || n.Association != (AssociationChange{State: 65534, Error: 19, OutboundStreams: 3, InboundStreams: 5, AssocID: -2}) || string(n.Tail) != "abc" {
			t.Fatal(n, e)
		}
		p[8] = 0
		p[20] = 'X'
		if n.Association.State != 65534 || string(n.Tail) != "Xbc" || cap(n.Tail) != len(n.Tail) {
			t.Fatal("fixed fields must be owned; tail borrowed", n)
		}
	})
	for _, v6 := range []bool{false, true} {
		t.Run(map[bool]string{false: "ipv4", true: "ipv6"}[v6], func(t *testing.T) {
			p := notificationFixture(SCTP_PEER_ADDR_CHANGE, 148)
			a := p[8:136]
			ip := netip.MustParseAddr("192.0.2.1")
			family := uint16(syscall.AF_INET)
			if v6 {
				ip = netip.MustParseAddr("fe80::1234")
				family = syscall.AF_INET6
				binary.BigEndian.PutUint32(a[4:], 0x12345)
				copy(a[8:24], ip.AsSlice())
				nativeEndian.PutUint32(a[24:], 7)
			} else {
				copy(a[4:8], ip.AsSlice())
			}
			nativeEndian.PutUint16(a, family)
			binary.BigEndian.PutUint16(a[2:], 3868)
			nativeEndian.PutUint32(p[136:], 0xffffffff)
			nativeEndian.PutUint32(p[140:], 0xfffffffe)
			nativeEndian.PutUint32(p[144:], 19)
			n, e := ParseNotification(p)
			if e != nil || n.Kind != NotificationPeerAddressChange || n.PeerAddress.Address.IP != ip || n.PeerAddress.Address.Port != 3868 || n.PeerAddress.Address.Family != family || n.PeerAddress.State != -1 || n.PeerAddress.Error != -2 || n.PeerAddress.AssocID != 19 {
				t.Fatal(n, e)
			}
			if v6 && (n.PeerAddress.Address.ScopeID != 7 || n.PeerAddress.Address.FlowInfo != 0x12345) {
				t.Fatal(n)
			}
			clear(p)
			if n.PeerAddress.Address.IP != ip {
				t.Fatal("address borrowed")
			}
		})
	}
	t.Run("send-failed", func(t *testing.T) {
		p := notificationFixture(SCTP_SEND_FAILED, 53)
		nativeEndian.PutUint16(p[2:], SendFailureDataSent)
		nativeEndian.PutUint32(p[8:], 41)
		b := p[12:44]
		nativeEndian.PutUint16(b, 2)
		nativeEndian.PutUint16(b[2:], 3)
		nativeEndian.PutUint16(b[4:], 4)
		binary.BigEndian.PutUint32(b[8:], 46)
		for i, v := range []uint32{12, 13, 14, 15, 16} {
			nativeEndian.PutUint32(b[12+i*4:], v)
		}
		nativeEndian.PutUint32(p[44:], 17)
		copy(p[48:], "hello")
		n, e := ParseNotification(p)
		want := SndRcvInfo{Stream: 2, SSN: 3, Flags: 4, PPID: 46, Context: 12, TTL: 13, TSN: 14, CumTSN: 15, AssocID: 16}
		if e != nil || n.Kind != NotificationSendFailure || n.SendFailure.Info != want || n.SendFailure.Error != 41 || n.SendFailure.AssocID != 17 || n.Header.Flags != SendFailureDataSent || string(n.Tail) != "hello" {
			t.Fatal(n, e)
		}
		p[48] = 'H'
		clear(p[12:44])
		if n.SendFailure.Info != want || string(n.Tail) != "Hello" {
			t.Fatal(n)
		}
	})
	t.Run("shutdown", func(t *testing.T) {
		p := notificationFixture(SCTP_SHUTDOWN_EVENT, 12)
		nativeEndian.PutUint32(p[8:], 8)
		n, e := ParseNotification(p)
		if e != nil || n.Kind != NotificationShutdown || n.Shutdown.AssocID != 8 || len(n.Tail) != 0 {
			t.Fatal(n, e)
		}
	})
	t.Run("partial", func(t *testing.T) {
		p := notificationFixture(SCTP_PARTIAL_DELIVERY_EVENT, 24)
		nativeEndian.PutUint32(p[8:], 123)
		nativeEndian.PutUint32(p[12:], 9)
		n, e := ParseNotification(p)
		if e != nil || n.Kind != NotificationPartialDelivery || n.PartialDelivery.Indication != 123 || n.PartialDelivery.AssocID != 9 || !n.PartialDelivery.StreamPresent || !n.PartialDelivery.SequencePresent || n.PartialDelivery.Stream != 0 || n.PartialDelivery.Sequence != 0 {
			t.Fatal(n, e)
		}
	})
}
func TestNotificationUnknownAndFailures(t *testing.T) {
	// New SEND_FAILED_EVENT must remain unknown, not decoded as old SNDRCV.
	for _, kind := range []SCTPNotificationType{SCTP_REMOTE_ERROR, SCTP_SN_TYPE_BASE + 13, 65535} {
		p := notificationFixture(kind, 13)
		nativeEndian.PutUint16(p[2:], 65534)
		copy(p[8:], "extra")
		n, e := ParseNotification(p)
		if e != nil || n.Kind != NotificationUnknown || n.Header.Type != uint16(kind) || n.Header.Flags != 65534 || string(n.Tail) != "extra" || cap(n.Tail) != len(n.Tail) {
			t.Fatal(n, e)
		}
	}
	for _, kind := range []SCTPNotificationType{0, SCTP_SN_TYPE_BASE} {
		if n, e := ParseNotification(notificationFixture(kind, 8)); !errors.Is(e, ErrMessageBoundary) || !reflect.DeepEqual(n, Notification{}) {
			t.Fatal(n, e)
		}
	}
	for _, tc := range []struct {
		kind SCTPNotificationType
		size int
	}{{SCTP_ASSOC_CHANGE, 20}, {SCTP_PEER_ADDR_CHANGE, 148}, {SCTP_SEND_FAILED, 48}, {SCTP_SHUTDOWN_EVENT, 12}, {SCTP_PARTIAL_DELIVERY_EVENT, 24}} {
		for size := 0; size < tc.size; size++ {
			p := make([]byte, size)
			if size >= 8 {
				p = notificationFixture(tc.kind, size)
			}
			n, e := ParseNotification(p)
			if !errors.Is(e, ErrMessageBoundary) || !reflect.DeepEqual(n, Notification{}) {
				t.Fatalf("kind=%d size=%d result=%+v err=%v", tc.kind, size, n, e)
			}
		}
		p := notificationFixture(tc.kind, tc.size)
		nativeEndian.PutUint32(p[4:], 0xffffffff)
		if _, e := ParseNotification(p); !errors.Is(e, ErrMessageBoundary) {
			t.Fatal(e)
		}
	}
	for _, kind := range []SCTPNotificationType{SCTP_PEER_ADDR_CHANGE, SCTP_SHUTDOWN_EVENT, SCTP_PARTIAL_DELIVERY_EVENT} {
		size := map[SCTPNotificationType]int{SCTP_PEER_ADDR_CHANGE: 148, SCTP_SHUTDOWN_EVENT: 12, SCTP_PARTIAL_DELIVERY_EVENT: 24}[kind]
		if _, e := ParseNotification(notificationFixture(kind, size+1)); !errors.Is(e, ErrMessageBoundary) {
			t.Fatal(e)
		}
	}
	if _, e := ParseNotification(notificationFixture(SCTP_PEER_ADDR_CHANGE, 148)); !errors.Is(e, ErrMessageBoundary) {
		t.Fatal("unknown family", e)
	}
}
func TestNotificationLargeTailNoAllocation(t *testing.T) {
	p := notificationFixture(SCTP_SEND_FAILED, 48+1<<20)
	copy(p[48:], bytes.Repeat([]byte{0x42}, len(p)-48))
	var n Notification
	var e error
	alloc := testing.AllocsPerRun(100, func() { n, e = ParseNotification(p) })
	if e != nil || alloc != 0 || len(n.Tail) != 1<<20 || cap(n.Tail) != len(n.Tail) {
		t.Fatal(alloc, len(n.Tail), e)
	}
}
func FuzzParseNotification(f *testing.F) {
	for _, tc := range []struct {
		k SCTPNotificationType
		n int
	}{{SCTP_ASSOC_CHANGE, 20}, {SCTP_SEND_FAILED, 48}, {SCTP_SHUTDOWN_EVENT, 12}, {SCTP_PARTIAL_DELIVERY_EVENT, 24}, {65535, 8}} {
		f.Add(notificationFixture(tc.k, tc.n))
	}
	p := notificationFixture(SCTP_PEER_ADDR_CHANGE, 148)
	nativeEndian.PutUint16(p[8:], syscall.AF_INET6)
	f.Add(p)
	f.Fuzz(func(t *testing.T, p []byte) {
		before := bytes.Clone(p)
		n, e := ParseNotification(p)
		if !bytes.Equal(before, p) {
			t.Fatal("input mutated")
		}
		if e != nil {
			if !errors.Is(e, ErrMessageBoundary) || !reflect.DeepEqual(n, Notification{}) {
				t.Fatal(n, e)
			}
			return
		}
		if uint64(n.Header.Length) != uint64(len(p)) || cap(n.Tail) != len(n.Tail) || len(n.Tail) > len(p) {
			t.Fatal(n)
		}
	})
}
