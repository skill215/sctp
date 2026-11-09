//go:build linux && !386

package sctp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func messagePair(t *testing.T, events uint32) (*SCTPConn, *SCTPConn) {
	t.Helper()
	opts := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, Events: events, NoDelay: true, AllowFragmentation: true}
	cfg := SocketConfig{Messages: &opts, InitMsg: InitMsg{NumOstreams: 3, MaxInstreams: 3}}
	ln, e := cfg.Listen("sctp4", &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, e := cfg.DialContext(ctx, "sctp4", nil, ln.Addr().(*SCTPAddr))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	s, e := ln.AcceptSCTP()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	opts.NoDelay = false // both constructors own their immutable configuration
	for _, x := range []*SCTPConn{c, s} {
		got, e := x.GetMessageOptions()
		if e != nil || !got.Options.NoDelay || got.Options.Events != events || got.Layout == "" {
			t.Fatal(got, e)
		}
		if e = x.SetDeadline(time.Now().Add(3 * time.Second)); e != nil {
			t.Fatal(e)
		}
	}
	return c, s
}
func messageBuffers(c *SCTPConn) ([]byte, []byte, []byte) {
	r, _ := MessageIORequirements(*c.messages)
	return make([]byte, 9), make([]byte, r.NotificationMin), make([]byte, r.OOBMin)
}

func TestMessageRealSCTPFragmentsAndNotifications(t *testing.T) {
	c, s := messagePair(t, SCTP_EVENT_ASSOCIATION|SCTP_EVENT_ADDRESS|SCTP_EVENT_SEND_FAILURE|SCTP_EVENT_SHUTDOWN|SCTP_EVENT_PARTIAL_DELIVERY)
	d, b, o := messageBuffers(s)
	ctx := context.Background()
	// Establishment notification must be returned before user data, never skipped.
	r, e := s.ReadEvent(ctx, d, b, o)
	if e != nil || r.Kind != ReadNotification || r.N < 20 || !r.Consumed || r.RecvFlags&syscall.MSG_EOR == 0 {
		t.Fatal(r, e)
	}
	if !bytes.Equal(d, make([]byte, len(d))) {
		t.Fatal("notification overwrote data")
	}
	for _, info := range []SndInfo{{}, {SID: 1, PPID: 46, Context: 123, Flags: SCTP_UNORDERED}} {
		payload := bytes.Repeat([]byte("fragment-"), 9)
		if n, e := c.WriteMsgContext(ctx, payload, info); e != nil || n != len(payload) {
			t.Fatal(n, e)
		}
		var assembled []byte
		parts := 0
		for {
			saved := bytes.Clone(b)
			r, e = s.ReadEvent(ctx, d, b, o)
			if e != nil || r.Kind != ReadData || !r.InfoPresent || r.Info.PPID != info.PPID || r.Info.Stream != info.SID || !r.Consumed {
				t.Fatal(r, e)
			}
			if !bytes.Equal(b, saved) {
				t.Fatal("data overwrote notification buffer")
			}
			parts++
			assembled = append(assembled, d[:r.N]...)
			if r.RecvFlags&syscall.MSG_EOR != 0 {
				break
			}
		}
		if parts < 2 || !bytes.Equal(assembled, payload) {
			t.Fatal(parts, string(assembled))
		}
	}
}
func TestMessageModesAndBufferAdmission(t *testing.T) {
	c, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	ctx := context.Background()
	checks := []func() error{
		func() error { _, e := s.Read(d); return e }, func() error { _, e := s.Write(d); return e },
		func() error { return s.SubscribeEvents(0) }, func() error { return s.enableDataIO() },
		func() error { return s.SetDefaultSentParam(&SndRcvInfo{}) }, func() error { _, _, e := s.Setsockopt(SCTP_EVENTS, 0, 0); return e }, func() error { _, e := s.PeelOff(0); return e },
	}
	for _, check := range checks {
		if e := check(); !errors.Is(e, ErrIOMode) {
			t.Fatal(e)
		}
	}
	for _, bufs := range [][3][]byte{{nil, b, o}, {d, b[:len(b)-1], o}, {d, b, o[:len(o)-1]}} {
		if _, e := s.ReadEvent(ctx, bufs[0], bufs[1], bufs[2]); !errors.Is(e, ErrShortBuffer) {
			t.Fatal(e)
		}
	}
	overlap := make([]byte, 100)
	if _, e := s.ReadEvent(ctx, overlap[:9], overlap[:8], o); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	if _, e := c.WriteMsgContext(ctx, nil, SndInfo{}); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	for _, info := range []SndInfo{{Flags: 0xffff}, {AssocID: c.messageAssoc + 1}} {
		if _, e := c.WriteMsgContext(ctx, d, info); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
	}
	if _, e := c.WriteMsgContext(ctx, []byte("ok"), SndInfo{}); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReadEvent(ctx, d, b, o)
	if e != nil || string(d[:r.N]) != "ok" {
		t.Fatal(r, e)
	}
	legacy := lifecycleListener(t)
	cc, e := DialSCTP("sctp4", nil, legacy.Addr().(*SCTPAddr))
	if e != nil {
		t.Fatal(e)
	}
	defer cc.Close()
	if _, e := cc.ReadEvent(ctx, d, b, o); !errors.Is(e, ErrIOMode) {
		t.Fatal(e)
	}
	if _, e := cc.WriteMsgContext(ctx, d, SndInfo{}); !errors.Is(e, ErrIOMode) {
		t.Fatal(e)
	}
}
func TestMessageConfigBeforeFD(t *testing.T) {
	valid := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV}
	for _, m := range []MessageOptions{{}, {ReceiveInfo: 2}, {ReceiveInfo: ReceiveInfoSNDRCV, FragmentInterleave: 1}, {ReceiveInfo: ReceiveInfoSNDRCV, Events: SCTP_EVENT_DATA_IO}, {ReceiveInfo: ReceiveInfoSNDRCV, Events: SCTP_EVENT_AUTHENTICATION}} {
		if _, e := MessageIORequirements(m); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
		cfg := SocketConfig{Messages: &m}
		if _, e := cfg.Listen("sctp4", nil); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
		if _, e := cfg.DialContext(context.Background(), "sctp4", nil, nil); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
	}
	cfg := SocketConfig{Messages: &valid, NotificationHandler: func([]byte) error { t.Fatal("handler invoked"); return nil }}
	if _, e := cfg.Listen("sctp4", nil); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	cfg.NotificationHandler = nil
	cfg.Control = func(string, string, syscall.RawConn) error { t.Fatal("Control invoked"); return nil }
	if _, e := cfg.Listen("sctp4", nil); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
}

func TestMessageAncillaryBoundaries(t *testing.T) {
	var tmp [64]byte
	valid := bytes.Clone(messageSendControl(tmp[:], SndInfo{SID: 2, PPID: 46, Context: 9, AssocID: 7}))
	info, p, e := messageInfo(valid)
	if e != nil || !p || info.PPID != 46 || info.Stream != 2 || info.Context != 9 || info.AssocID != 7 || info.TTL != 0 {
		t.Fatal(info, p, e)
	}
	unknown := bytes.Clone(valid)
	word := syscall.CmsgLen(0) - 8
	nativeEndian.PutUint32(unknown[word+4:], 99)
	info, p, e = messageInfo(append(unknown, valid...))
	if e != nil || !p || info.PPID != 46 {
		t.Fatal(info, p, e)
	}
	if _, p, e = messageInfo(nil); p || e != nil {
		t.Fatal(p, e)
	}
	for _, bad := range [][]byte{valid[:1], valid[:len(valid)-1], append(bytes.Clone(valid), valid...)} {
		if _, _, e = messageInfo(bad); !errors.Is(e, ErrMessageBoundary) {
			t.Fatal(e)
		}
	}
	for _, flags := range []int{syscall.MSG_TRUNC, syscall.MSG_CTRUNC, MSG_NOTIFICATION} {
		r, e := messageReceived(false, 0, []byte("abc"), valid, 3, len(valid), flags, nil)
		if !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired || !r.Consumed || r.N != 3 || r.RecvFlags != flags {
			t.Fatal(r, e)
		}
	}
	r, e := messageReceived(false, 0, []byte("abc"), nil, 2, 0, syscall.MSG_EOR, syscall.EIO)
	if !errors.Is(e, syscall.EIO) || r.N != 2 || !r.Consumed || !r.CloseRequired {
		t.Fatal(r, e)
	}
}

// Faults use per-call injection; no global hooks or exported test knobs.
func TestMessageOversizeRetryAndReadLatch(t *testing.T) {
	_, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	calls := 0
	recv := func(_ int, p, o []byte, flags int) (int, int, int, error) {
		calls++
		nativeEndian.PutUint32(p[4:], 32)
		if flags == syscall.MSG_PEEK {
			return 8, 0, MSG_NOTIFICATION | syscall.MSG_TRUNC, nil
		}
		return 32, 0, MSG_NOTIFICATION | syscall.MSG_EOR, nil
	}
	r, e := s.readEvent(context.Background(), d, b, o, recv)
	if !errors.Is(e, ErrNotificationTooLarge) || calls != 1 || r.Consumed || r.CloseRequired || r.N != 0 || r.RecvFlags != 0 || r.RequiredNotificationBytes != 32 {
		t.Fatal(r, e, calls)
	}
	b = make([]byte, 32)
	r, e = s.readEvent(context.Background(), d, b, o, recv)
	if e != nil || calls != 3 || !r.Consumed || r.N != 32 {
		t.Fatal(r, e, calls)
	}
	bad := func(_ int, p, o []byte, flags int) (int, int, int, error) {
		calls++
		if flags == syscall.MSG_PEEK {
			return 1, 0, 0, nil
		}
		return 1, 0, syscall.MSG_CTRUNC, nil
	}
	r, e = s.readEvent(context.Background(), d, b, o, bad)
	if !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired || !r.Consumed {
		t.Fatal(r, e)
	}
	before := calls
	r, e = s.readEvent(context.Background(), d, b, o, bad)
	if !errors.Is(e, ErrReadUnusable) || !r.CloseRequired || r.Consumed || calls != before {
		t.Fatal(r, e, calls)
	}
}
func TestMessageCancelBetweenPeekAndConsume(t *testing.T) {
	c, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r, e := s.readEvent(ctx, d, b, o, func(_ int, p, o []byte, flags int) (int, int, int, error) { calls++; cancel(); return 1, 0, 0, nil })
	if !errors.Is(e, context.Canceled) || r.Consumed || calls != 1 {
		t.Fatal(r, e, calls)
	}
	if _, e = c.WriteMsgContext(context.Background(), []byte("fresh"), SndInfo{}); e != nil {
		t.Fatal(e)
	}
	r, e = s.ReadEvent(context.Background(), d, b, o)
	if e != nil || string(d[:r.N]) != "fresh" {
		t.Fatal(r, e)
	}
}
func TestMessageWriteSingleAttemptFaults(t *testing.T) {
	c, _ := messagePair(t, 0)
	for _, tc := range []struct {
		n    int
		err  error
		want error
	}{{2, nil, io.ErrShortWrite}, {2, syscall.EIO, syscall.EIO}, {0, syscall.EPIPE, syscall.EPIPE}} {
		calls := 0
		n, e := c.writeMsgContext(context.Background(), []byte("longer"), SndInfo{}, func(_ int, p, o []byte, _ syscall.Sockaddr, _ int) (int, error) {
			calls++
			info, present, e := messageInfo(o)
			if e != nil || !present || info.TTL != 0 {
				t.Fatal(info, present, e)
			}
			return tc.n, tc.err
		})
		if calls != 1 || n != tc.n || !errors.Is(e, tc.want) {
			t.Fatal(calls, n, e)
		}
	}
}
func TestMessageDeadlinesCancelAndClose(t *testing.T) {
	_, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	s.SetReadDeadline(time.Now().Add(-time.Second))
	if r, e := s.ReadEvent(context.Background(), d, b, o); !errors.Is(e, os.ErrDeadlineExceeded) || r.Consumed {
		t.Fatal(r, e)
	}
	s.SetReadDeadline(time.Time{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := s.ReadEvent(ctx, d, b, o); done <- e }()
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	go func() { _, e := s.ReadEvent(context.Background(), d, b, o); done <- e }()
	s.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("close lost")
		}
	case <-time.After(time.Second):
		t.Fatal("read blocked on close")
	}
}

func TestMessageQueuedCancelAndActiveDeadlineUpdate(t *testing.T) {
	c, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, e := s.readEvent(context.Background(), d, b, o, func(_ int, _, _ []byte, flags int) (int, int, int, error) {
			close(entered)
			<-release
			return 0, 0, 0, syscall.EAGAIN
		})
		first <- e
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d2, b2, o2 := messageBuffers(s)
	if r, e := s.ReadEvent(ctx, d2, b2, o2); !errors.Is(e, context.Canceled) || r.Consumed {
		t.Fatal(r, e)
	}
	if e := s.SetReadDeadline(time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-first; !errors.Is(e, os.ErrDeadlineExceeded) {
		t.Fatal(e)
	}
	s.SetReadDeadline(time.Time{})
	if _, e := c.WriteMsgContext(context.Background(), []byte("after"), SndInfo{}); e != nil {
		t.Fatal(e)
	}
	r, e := s.ReadEvent(context.Background(), d, b, o)
	if e != nil || string(d[:r.N]) != "after" {
		t.Fatal(r, e)
	}
}
func TestMessageNotificationFailuresAndEOF(t *testing.T) {
	for _, tc := range []struct {
		name     string
		peekN    int
		declared uint32
		flags    int
		n        int
		bad      bool
	}{
		{"short-peek", 4, 16, MSG_NOTIFICATION, 0, true},
		{"short-header", 8, 7, MSG_NOTIFICATION, 0, true},
		{"partial-notification", 8, 16, MSG_NOTIFICATION, 8, true},
		{"missing-eor", 8, 16, MSG_NOTIFICATION, 16, true},
		{"changed-kind", 8, 16, syscall.MSG_EOR, 16, true},
		{"unknown-raw", 8, 16, MSG_NOTIFICATION | syscall.MSG_EOR, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s := messagePair(t, 0)
			d, _, o := messageBuffers(s)
			b := make([]byte, 16)
			r, e := s.readEvent(context.Background(), d, b, o, func(_ int, p, o []byte, flags int) (int, int, int, error) {
				nativeEndian.PutUint16(p, 65530)
				nativeEndian.PutUint32(p[4:], tc.declared)
				if flags == syscall.MSG_PEEK {
					return tc.peekN, 0, MSG_NOTIFICATION, nil
				}
				return tc.n, 0, tc.flags, nil
			})
			if tc.bad {
				if !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired {
					t.Fatal(r, e)
				}
			} else if e != nil || r.Kind != ReadNotification || r.N != 16 {
				t.Fatal(r, e)
			}
		})
	}
	_, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	r, e := s.readEvent(context.Background(), d, b, o, func(int, []byte, []byte, int) (int, int, int, error) { return 0, 0, 0, nil })
	if !errors.Is(e, io.EOF) || r.Kind != ReadEOF || r.Consumed || r.CloseRequired {
		t.Fatal(r, e)
	}
}
