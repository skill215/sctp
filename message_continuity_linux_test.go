//go:build linux && !386

package sctp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func injectedRecord(t *testing.T, c *SCTPConn, info SndRcvInfo, present, eor bool, notify []byte) (ReadResult, error) {
	t.Helper()
	d, b, o := messageBuffers(c)
	if len(notify) > len(b) {
		b = make([]byte, len(notify))
	}
	return c.readEvent(context.Background(), d, b, o, func(_ int, p, o []byte, flags int) (int, int, int, error) {
		if notify != nil {
			copy(p, notify)
			f := MSG_NOTIFICATION | syscall.MSG_EOR
			if flags == syscall.MSG_PEEK {
				return min(8, len(notify)), 0, f, nil
			}
			return len(notify), 0, f, nil
		}
		if flags == syscall.MSG_PEEK {
			return 1, 0, 0, nil
		}
		p[0] = 42
		on := 0
		if present {
			on = len(messageSendControl(o, SndInfo{SID: info.Stream, Flags: info.Flags, PPID: info.PPID, AssocID: info.AssocID}))
			nativeEndian.PutUint16(o[syscall.CmsgLen(0)+2:], info.SSN)
		}
		f := 0
		if eor {
			f = syscall.MSG_EOR
		}
		return 1, on, f, nil
	})
}
func TestMessageContinuityFaultMatrix(t *testing.T) {
	for _, bad := range []string{"stream", "assoc", "ppid", "ordered", "ssn", "missing", "inbound"} {
		t.Run(bad, func(t *testing.T) {
			_, c := messagePair(t, 0)
			i := SndRcvInfo{AssocID: c.messageAssoc, SSN: 17, PPID: 46}
			r, e := injectedRecord(t, c, i, true, false, nil)
			if e != nil || r.EndOfRecord() {
				t.Fatal(r, e)
			}
			switch bad {
			case "stream":
				i.Stream = 1
			case "assoc":
				i.AssocID++
			case "ppid":
				i.PPID = 0
			case "ordered":
				i.Flags = SCTP_UNORDERED
			case "ssn":
				i.SSN++
			case "inbound":
				i.Stream = c.messageIn
			}
			r, e = injectedRecord(t, c, i, bad != "missing", true, nil)
			if !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired || r.N != 1 || !r.Consumed || !r.EndOfRecord() {
				t.Fatal(r, e)
			}
			d, b, o := messageBuffers(c)
			r, e = c.readEvent(context.Background(), d, b, o, func(int, []byte, []byte, int) (int, int, int, error) {
				t.Fatal("read after unusable")
				return 0, 0, 0, nil
			})
			if !errors.Is(e, ErrReadUnusable) || !r.CloseRequired || r.Consumed {
				t.Fatal(r, e)
			}
		})
	}
}
func TestMessageContinuityNotificationPositionsAndSwitches(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		for at := 0; at <= 3; at++ {
			_, c := messagePair(t, 0)
			i := SndRcvInfo{AssocID: c.messageAssoc, SSN: 12, PPID: 46}
			if unordered {
				i.Flags = SCTP_UNORDERED
			}
			notice := make([]byte, 8)
			nativeEndian.PutUint16(notice, 0xfffa)
			nativeEndian.PutUint32(notice[4:], 8)
			for part := 0; part <= 3; part++ {
				if at == part {
					r, e := injectedRecord(t, c, i, true, true, notice)
					if e != nil || r.Kind != ReadNotification {
						t.Fatal(r, e)
					}
					if c.messageCursor.active != (part > 0 && part < 3) {
						t.Fatal("notification reset cursor")
					}
				}
				if part == 3 {
					break
				}
				if unordered {
					i.SSN++
				}
				r, e := injectedRecord(t, c, i, true, part == 2, nil)
				if e != nil || r.EndOfRecord() != (part == 2) {
					t.Fatal(r, e)
				}
			}
			i.Stream = 2
			i.PPID = 0
			i.SSN = 99
			i.Flags = 0
			if r, e := injectedRecord(t, c, i, true, true, nil); e != nil || !r.EndOfRecord() {
				t.Fatal("new message switch", r, e)
			}
		}
	}
}
func TestMessageContinuityCancellationAndEOF(t *testing.T) {
	c, s := messagePair(t, 0)
	p := bytes.Repeat([]byte{7}, 20)
	if _, e := c.WriteMsgContext(context.Background(), p, SndInfo{}); e != nil {
		t.Fatal(e)
	}
	d, b, o := messageBuffers(s)
	r, e := s.ReadEvent(context.Background(), d, b, o)
	if e != nil || r.EndOfRecord() || !s.messageCursor.active {
		t.Fatal(r, e)
	}
	cursor := s.messageCursor
	ctx, cancel := context.WithCancel(context.Background())
	r, e = s.readEvent(ctx, d, b, o, func(int, []byte, []byte, int) (int, int, int, error) { cancel(); return 1, 0, syscall.MSG_EOR, nil })
	if !errors.Is(e, context.Canceled) || r.Consumed || r.EndOfRecord() || s.messageCursor != cursor {
		t.Fatal(r, e)
	}
	for s.messageCursor.active {
		if r, e = s.ReadEvent(context.Background(), d, b, o); e != nil {
			t.Fatal(r, e)
		}
	}
	// An EOF after prior partial consumption cannot claim a recoverable boundary.
	_, e = injectedRecord(t, s, SndRcvInfo{AssocID: s.messageAssoc}, true, false, nil)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.readEvent(context.Background(), d, b, o, func(int, []byte, []byte, int) (int, int, int, error) { return 0, 0, 0, nil })
	if !errors.Is(e, io.EOF) || !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired {
		t.Fatal(r, e)
	}
}
func TestMessageRestartInvalidatesSnapshot(t *testing.T) {
	c, _ := messagePair(t, 0)
	n := make([]byte, 20)
	nativeEndian.PutUint16(n, uint16(SCTP_ASSOC_CHANGE))
	nativeEndian.PutUint32(n[4:], 20)
	nativeEndian.PutUint16(n[8:], uint16(SCTP_RESTART))
	nativeEndian.PutUint32(n[16:], uint32(c.messageAssoc))
	r, e := injectedRecord(t, c, SndRcvInfo{}, false, true, n)
	if !errors.Is(e, ErrAssociationChanged) || r.N != 20 || r.Kind != ReadNotification || !r.CloseRequired {
		t.Fatal(r, e)
	}
	if n, e := c.WriteMsgContext(context.Background(), []byte("no"), SndInfo{}); n != 0 || !errors.Is(e, ErrAssociationChanged) {
		t.Fatal(n, e)
	}
}
func TestMessageNativeAsymmetricStreamsAndPPID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := boundedTestConfig()
	server.Socket.InitMsg = InitMsg{NumOstreams: 5, MaxInstreams: 3}
	ln, e := ListenContext(ctx, "sctp4", "127.0.0.1:0", server)
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	a, _ := ln.BoundAddr()
	client := boundedTestConfig()
	client.Socket.InitMsg = InitMsg{NumOstreams: 7, MaxInstreams: 2}
	dial, e := NewDialer("sctp4", "", a.String(), client)
	if e != nil {
		t.Fatal(e)
	}
	c, e := dial.DialContext(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	s, e := ln.AcceptSCTPContext(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	cs, e := c.GetStatus()
	if e != nil {
		t.Fatal(e)
	}
	ss, e := s.GetStatus()
	if e != nil {
		t.Fatal(e)
	}
	if cs.Ostreams != 3 || cs.Instreams != 2 || ss.Ostreams != 2 || ss.Instreams != 3 {
		t.Fatal(cs, ss)
	}
	for _, sender := range []*SCTPConn{c, s} {
		receiver := s
		if sender == s {
			receiver = c
		}
		for _, ppid := range []uint32{0, 46} {
			for _, flags := range []uint16{0, SCTP_UNORDERED} {
				info := SndInfo{SID: sender.messageOut - 1, PPID: ppid, Flags: flags}
				payload := bytes.Repeat([]byte{byte(ppid)}, 25)
				if n, e := sender.WriteMsgContext(ctx, payload, info); e != nil || n != len(payload) {
					t.Fatal(n, e)
				}
				data, b, o := messageBuffers(receiver)
				var got []byte
				for {
					r, e := receiver.ReadEvent(ctx, data, b, o)
					if e != nil || r.Info.PPID != ppid || r.Info.Stream != info.SID || r.Info.Flags&SCTP_UNORDERED != flags {
						t.Fatal(r, e)
					}
					got = append(got, data[:r.N]...)
					if r.EndOfRecord() {
						break
					}
				}
				if !bytes.Equal(got, payload) {
					t.Fatal("payload mismatch")
				}
			}
		}
		if n, e := sender.writeMsgContext(ctx, []byte{1}, SndInfo{SID: sender.messageOut}, func(int, []byte, []byte, syscall.Sockaddr, int) (int, error) {
			t.Fatal("invalid SID reached send")
			return 1, nil
		}); n != 0 || !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(n, e)
		}
	}
	// Client has three outgoing but only two incoming streams: receive must use
	// the latter. Server already received stream2 successfully in the native path.
	r, e := injectedRecord(t, c, SndRcvInfo{AssocID: c.messageAssoc, Stream: 2}, true, true, nil)
	if !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired {
		t.Fatal(r, e)
	}
	t.Logf("client in/out=%d/%d server in/out=%d/%d", cs.Instreams, cs.Ostreams, ss.Instreams, ss.Ostreams)
}
func TestMessageCommittedWriteEvidenceUnderCancelClose(t *testing.T) {
	for _, closeIt := range []bool{false, true} {
		for _, cause := range []error{nil, syscall.EIO} {
			c, _ := messagePair(t, 0)
			ctx, cancel := context.WithCancel(context.Background())
			closed := make(chan error, 1)
			calls := 0
			n, e := c.writeMsgContext(ctx, []byte("long"), SndInfo{PPID: 46}, func(int, []byte, []byte, syscall.Sockaddr, int) (int, error) {
				calls++
				cancel()
				if closeIt {
					go func() { closed <- c.Close() }()
					<-c.io.closing
				}
				return 2, cause
			})
			want := cause
			if want == nil {
				want = io.ErrShortWrite
			}
			if n != 2 || !errors.Is(e, want) || calls != 1 {
				t.Fatal(n, e, calls)
			}
			if closeIt {
				if e := deadlineResult(t, closed); e != nil {
					t.Fatal(e)
				}
			}
			u5Quiescent(t, c)
		}
	}
}

func TestMessageContinuityPartialAbortLatches(t *testing.T) {
	_, c := messagePair(t, 0)
	i := SndRcvInfo{AssocID: c.messageAssoc, Flags: SCTP_UNORDERED}
	if _, e := injectedRecord(t, c, i, true, false, nil); e != nil {
		t.Fatal(e)
	}
	n := make([]byte, 24)
	nativeEndian.PutUint16(n, uint16(SCTP_PARTIAL_DELIVERY_EVENT))
	nativeEndian.PutUint32(n[4:], 24)
	nativeEndian.PutUint32(n[8:], PartialDeliveryAborted)
	nativeEndian.PutUint32(n[12:], uint32(c.messageAssoc))
	r, e := injectedRecord(t, c, i, true, true, n)
	if e != nil || !r.CloseRequired || !r.Consumed || r.N != 24 || r.Kind != ReadNotification || !c.messageCursor.active {
		t.Fatal(r, e)
	}
	// A coincidentally matching unordered stream/PPID cannot join the aborted message.
	r, e = injectedRecord(t, c, i, true, true, nil)
	if !errors.Is(e, ErrReadUnusable) || r.Consumed || !r.CloseRequired {
		t.Fatal(r, e)
	}
}

func TestMessageContinuityConsumeFailureAfterPeek(t *testing.T) {
	for _, cause := range []error{syscall.ECONNRESET, syscall.EIO} {
		_, c := messagePair(t, 0)
		info := SndRcvInfo{AssocID: c.messageAssoc}
		if _, e := injectedRecord(t, c, info, true, false, nil); e != nil {
			t.Fatal(e)
		}
		data, notification, oob := messageBuffers(c)
		calls := 0
		r, e := c.readEvent(context.Background(), data, notification, oob, func(_ int, _, _ []byte, flags int) (int, int, int, error) {
			calls++
			if flags == syscall.MSG_PEEK {
				return 1, 0, 0, nil
			}
			return 0, 0, 0, cause
		})
		if !errors.Is(e, cause) || !errors.Is(e, ErrMessageBoundary) || !r.CloseRequired || r.Consumed || r.N != 0 || r.RecvFlags != 0 || calls != 2 {
			t.Fatalf("partial-message consume failure: result=%+v err=%v calls=%d", r, e, calls)
		}
		if !c.messageCursor.active {
			t.Fatal("failure reset the unfinished cursor")
		}
		r, e = c.readEvent(context.Background(), data, notification, oob, func(int, []byte, []byte, int) (int, int, int, error) {
			t.Fatal("unusable read reached syscall")
			return 0, 0, 0, nil
		})
		if !errors.Is(e, ErrReadUnusable) || !r.CloseRequired || r.Consumed {
			t.Fatal(r, e)
		}
	}
}
