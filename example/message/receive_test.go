//go:build linux && !386

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/skill215/sctp"
)

type event struct {
	result sctp.ReadResult
	data   []byte
	err    error
}

type scriptedReader struct {
	t      *testing.T
	ctx    context.Context
	events []event
}

func (s *scriptedReader) ReadEvent(ctx context.Context, data, notification, oob []byte) (sctp.ReadResult, error) {
	s.t.Helper()
	if ctx != s.ctx {
		s.t.Fatal("fragment/notification read changed the original context budget")
	}
	if len(s.events) == 0 {
		return sctp.ReadResult{}, io.ErrUnexpectedEOF
	}
	e := s.events[0]
	s.events = s.events[1:]
	if e.result.Kind == sctp.ReadNotification {
		copy(notification, e.data)
	} else {
		copy(data, e.data)
	}
	return e.result, e.err
}

func dataEvent(p string, end bool) event {
	r := sctp.ReadResult{Kind: sctp.ReadData, N: len(p), Consumed: true, InfoPresent: true,
		Info: sctp.SndRcvInfo{Stream: 1, PPID: 46}}
	if end {
		r.RecvFlags = 0x80 // native EOR used by ReadResult.EndOfRecord
	}
	return event{result: r, data: []byte(p)}
}

func notificationEvent(kind uint16, size int) event {
	p := make([]byte, size)
	binary.NativeEndian.PutUint16(p, kind)
	binary.NativeEndian.PutUint32(p[4:], uint32(size))
	return event{result: sctp.ReadResult{Kind: sctp.ReadNotification, N: size, Consumed: true, RecvFlags: 0x80}, data: p}
}

func TestReceiveKeepsFragmentsAndBudgetAcrossNotifications(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	r := newReader(4, 8, sctp.IORequirements{})
	seen := 0
	r.onNotification = func(n sctp.Notification) { seen++ }
	s := &scriptedReader{t: t, ctx: ctx, events: []event{
		dataEvent("ab", false), notificationEvent(uint16(sctp.SCTP_ASSOC_CHANGE), 20), dataEvent("cd", true),
	}}
	p, info, err := r.receive(ctx, s)
	if err != nil || string(p) != "abcd" || info.Stream != 1 || info.PPID != 46 || seen != 1 || len(s.events) != 0 {
		t.Fatalf("payload=%q info=%+v notifications=%d remaining=%d err=%v", p, info, seen, len(s.events), err)
	}
}

func TestReceiveTerminalNotificationBeforeClosure(t *testing.T) {
	restart := notificationEvent(uint16(sctp.SCTP_ASSOC_CHANGE), 20)
	binary.NativeEndian.PutUint16(restart.data[8:], uint16(sctp.SCTP_RESTART))
	restart.result.CloseRequired, restart.err = true, sctp.ErrAssociationChanged
	abort := notificationEvent(uint16(sctp.SCTP_PARTIAL_DELIVERY_EVENT), 24)
	abort.result.CloseRequired = true
	malformed := restart
	malformed.err = errors.Join(sctp.ErrAssociationChanged, sctp.ErrMessageBoundary)
	unconsumed := restart
	unconsumed.result.Consumed = false
	oversized := event{result: sctp.ReadResult{Kind: sctp.ReadNotification, RequiredNotificationBytes: 8192}, err: sctp.ErrNotificationTooLarge}
	for _, tc := range []struct {
		name string
		e    event
		seen int
		want error
	}{
		{"restart", restart, 1, sctp.ErrAssociationChanged},
		{"partial-abort", abort, 1, sctp.ErrReadUnusable},
		{"malformed-restart", malformed, 0, sctp.ErrMessageBoundary},
		{"unconsumed", unconsumed, 0, sctp.ErrAssociationChanged},
		{"oversized", oversized, 0, sctp.ErrNotificationTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newReader(4, 8, sctp.IORequirements{})
			seen := 0
			r.onNotification = func(n sctp.Notification) { seen++ }
			s := &scriptedReader{t: t, ctx: t.Context(), events: []event{dataEvent("ab", false), tc.e, dataEvent("cd", true)}}
			p, _, err := r.receive(s.ctx, s)
			if p != nil || !errors.Is(err, tc.want) || seen != tc.seen || len(s.events) != 1 {
				t.Fatalf("payload=%q notifications=%d remaining=%d err=%v", p, seen, len(s.events), err)
			}
		})
	}
}

func TestReceiveApplicationLimits(t *testing.T) {
	for _, notifications := range []bool{false, true} {
		r := newReader(4, 3, sctp.IORequirements{})
		r.onNotification = func(sctp.Notification) {}
		events := []event{dataEvent("ab", false), dataEvent("cd", true)}
		want := "message exceeds"
		if notifications {
			events = nil
			for range 65 {
				events = append(events, notificationEvent(uint16(sctp.SCTP_ASSOC_CHANGE), 20))
			}
			want = "notification budget"
		}
		s := &scriptedReader{t: t, ctx: t.Context(), events: events}
		p, _, err := r.receive(s.ctx, s)
		if p != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("payload=%q err=%v, want %s", p, err, want)
		}
	}
}
