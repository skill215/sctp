//go:build linux && !386

package sctp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Test-only raw control deliberately creates kernel events/faults. It does not
// add public mode mutation or unreliable-send flags to WriteMsgContext.
func notificationSetInt(t *testing.T, c *SCTPConn, level, opt, value int) {
	t.Helper()
	if e := c.withFD(func(fd int) error { return syscall.SetsockoptInt(fd, level, opt, value) }); e != nil {
		t.Fatal(e)
	}
}
func notificationAbort(t *testing.T, c *SCTPConn) {
	t.Helper()
	if e := c.withFD(func(fd int) error {
		return syscall.SetsockoptLinger(fd, syscall.SOL_SOCKET, syscall.SO_LINGER, &syscall.Linger{Onoff: 1})
	}); e != nil {
		t.Fatal(e)
	}
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
}
func notificationRead(t *testing.T, c *SCTPConn) (ReadResult, Notification) {
	t.Helper()
	d := make([]byte, 20)
	b := make([]byte, 1<<20)
	o := make([]byte, 128)
	r, e := c.ReadEvent(context.Background(), d, b, o)
	if e != nil {
		t.Fatal(r, e)
	}
	if r.Kind != ReadNotification {
		t.Fatal("expected notification", r)
	}
	n, e := ParseNotification(b[:r.N])
	if e != nil {
		t.Fatal(r, e)
	}
	return r, n
}

func TestNotificationNativeAssociationShutdown(t *testing.T) {
	c, s := messagePair(t, SCTP_EVENT_ASSOCIATION|SCTP_EVENT_SHUTDOWN)
	for _, conn := range []*SCTPConn{c, s} {
		_, n := notificationRead(t, conn)
		status, e := conn.GetStatus()
		if e != nil || n.Kind != NotificationAssociationChange || n.Association.State != SCTP_COMM_UP || n.Association.AssocID != status.AssocID || n.Association.OutboundStreams != status.Ostreams || n.Association.InboundStreams != status.Instreams {
			t.Fatal(n, status, e)
		}
	}
	if e := c.Close(); e != nil {
		t.Fatal(e)
	}
	_, n := notificationRead(t, s)
	if n.Kind != NotificationShutdown || int32(n.Shutdown.AssocID) != s.messageAssoc {
		t.Fatal(n)
	}
}
func TestNotificationNativeLargeSendFailure(t *testing.T) {
	c, s := messagePair(t, SCTP_EVENT_SEND_FAILURE)
	notificationSetInt(t, c, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1<<20)
	notificationSetInt(t, s, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096)
	payload := bytes.Repeat([]byte{0x5a}, 256<<10)
	n, e := c.WriteMsgContext(context.Background(), payload, SndInfo{SID: 1, PPID: 46, Context: 0x12345678})
	if e != nil || n != len(payload) {
		t.Fatal(n, e)
	}
	notificationAbort(t, s)
	d := make([]byte, 1)
	b := make([]byte, 48)
	o := make([]byte, 128)
	r, e := c.ReadEvent(context.Background(), d, b, o)
	if !errors.Is(e, ErrNotificationTooLarge) || r.RequiredNotificationBytes <= 48 || r.RequiredNotificationBytes > 1<<20 || r.Consumed || r.N != 0 || r.RecvFlags != 0 {
		t.Fatal(r, e)
	}
	b = make([]byte, int(r.RequiredNotificationBytes))
	want := r.RequiredNotificationBytes
	r, e = c.ReadEvent(context.Background(), d, b, o)
	if e != nil || r.Kind != ReadNotification || uint32(r.N) != want || !r.Consumed {
		t.Fatal(r, e)
	}
	event, e := ParseNotification(b[:r.N])
	if e != nil || event.Kind != NotificationSendFailure || event.SendFailure.Info.PPID != 46 || event.SendFailure.Info.Stream != 1 || event.SendFailure.Info.Context != 0x12345678 || int32(event.SendFailure.AssocID) != c.messageAssoc || len(event.Tail) == 0 || !bytes.Equal(event.Tail, bytes.Repeat([]byte{0x5a}, len(event.Tail))) {
		t.Fatal(event, e)
	}
	if event.Header.Flags != SendFailureDataSent && event.Header.Flags != SendFailureDataUnsent {
		t.Fatal(event.Header)
	}
	t.Logf("native old SEND_FAILED bytes=%d tail=%d flags=%d error=%d", r.N, len(event.Tail), event.Header.Flags, event.SendFailure.Error)
}
func TestNotificationNativePeerAddress(t *testing.T) {
	c, s := messagePair(t, SCTP_EVENT_ADDRESS)
	// SCTP_PRIMARY_ADDR explicitly selects this known peer path and causes the
	// native ADDR_MADE_PRIM notification. No network namespace or route mutation.
	peer := s.LocalAddr().(*SCTPAddr)
	var prim [132]byte
	nativeEndian.PutUint32(prim[:], uint32(c.messageAssoc))
	nativeEndian.PutUint16(prim[4:], syscall.AF_INET)
	prim[6] = byte(peer.Port >> 8)
	prim[7] = byte(peer.Port)
	copy(prim[8:12], peer.IPAddrs[0].IP.To4())
	if e := c.withFD(func(fd int) error {
		_, _, e := setsockopt(fd, SCTP_PRIMARY_ADDR, uintptr(unsafe.Pointer(&prim[0])), uintptr(len(prim)))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	r, n := notificationRead(t, c)
	if r.N != 148 || n.Kind != NotificationPeerAddressChange || int32(n.PeerAddress.AssocID) != c.messageAssoc || n.PeerAddress.Address.Port != uint16(peer.Port) {
		t.Fatal(r, n)
	}
	t.Logf("native peer address state=%d bytes=%d", n.PeerAddress.State, r.N)
}
func TestNotificationNativePartialAbort(t *testing.T) {
	opts := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, Events: SCTP_EVENT_PARTIAL_DELIVERY, NoDelay: true, AllowFragmentation: true}
	cfg := SocketConfig{Messages: &opts}
	ln, e := cfg.Listen("sctp4", &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}})
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	if e := ln.io.Control(func(fd uintptr) { e = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096) }); e != nil {
		t.Fatal(e)
	}
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, e := cfg.DialContext(ctx, "sctp4", nil, ln.Addr().(*SCTPAddr))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s, e := ln.AcceptSCTP()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	s.SetDeadline(time.Now().Add(3 * time.Second))
	notificationSetInt(t, s, SOL_SCTP, 19, 1024) // PARTIAL_DELIVERY_POINT, test only
	notificationSetInt(t, c, SOL_SCTP, SCTP_MAXSEG, 512)
	notificationSetInt(t, c, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1<<20)
	if n, e := c.WriteMsgContext(ctx, bytes.Repeat([]byte{0x7a}, 256<<10), SndInfo{}); e != nil || n != 256<<10 {
		t.Fatal(n, e)
	}
	d := make([]byte, 4096)
	b := make([]byte, 128)
	o := make([]byte, 128)
	r, e := s.ReadEvent(ctx, d, b, o)
	if e != nil || r.Kind != ReadData || r.N == 0 || r.RecvFlags&syscall.MSG_EOR != 0 {
		t.Fatal(r, e)
	}
	notificationAbort(t, c)
	for i := 0; i < 256; i++ {
		r, e = s.ReadEvent(ctx, d, b, o)
		if e != nil {
			t.Fatal(r, e)
		}
		if r.Kind == ReadData {
			if r.RecvFlags&syscall.MSG_EOR != 0 {
				t.Fatal("entire message completed before abort")
			}
			continue
		}
		n, e := ParseNotification(b[:r.N])
		if e != nil || n.Kind != NotificationPartialDelivery || n.PartialDelivery.Indication != PartialDeliveryAborted || int32(n.PartialDelivery.AssocID) != s.messageAssoc || !n.PartialDelivery.StreamPresent || !n.PartialDelivery.SequencePresent {
			t.Fatal(n, e)
		}
		t.Logf("native partial abort bytes=%d stream=%d sequence=%d", r.N, n.PartialDelivery.Stream, n.PartialDelivery.Sequence)
		return
	}
	t.Fatal("partial notification exceeded finite receive bound")
}
