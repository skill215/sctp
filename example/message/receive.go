package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/skill215/sctp"
)

type eventReader interface {
	ReadEvent(context.Context, []byte, []byte, []byte) (sctp.ReadResult, error)
}

type reader struct {
	data, notification, oob, message []byte
	onNotification                   func(sctp.Notification)
}

func newReader(chunk, limit int, req sctp.IORequirements) *reader {
	return &reader{
		data: make([]byte, chunk), notification: make([]byte, max(4096, req.NotificationMin)),
		oob: make([]byte, req.OOBMin), message: make([]byte, 0, limit),
		onNotification: func(n sctp.Notification) {
			// Fixed fields are owned. Do not retain n.Tail across the next read.
			log.Printf("notification kind=%d association=%+v shutdown=%+v partial=%+v send-failure=%+v peer-address=%+v",
				n.Kind, n.Association, n.Shutdown, n.PartialDelivery, n.SendFailure, n.PeerAddress)
		},
	}
}

// receive assembles one bounded application message. The returned slice is
// borrowed until the next call. On any error the caller closes the connection;
// this example does not attempt to resume partial application messages.
func (b *reader) receive(ctx context.Context, conn eventReader) ([]byte, sctp.SndRcvInfo, error) {
	b.message = b.message[:0]
	var info sctp.SndRcvInfo
	notifications := 0
	for {
		r, err := conn.ReadEvent(ctx, b.data, b.notification, b.oob)
		if r.Kind == sctp.ReadNotification && r.Consumed && r.EndOfRecord() &&
			r.N >= 8 && r.N <= len(b.notification) && !errors.Is(err, sctp.ErrMessageBoundary) &&
			(err == nil || errors.Is(err, sctp.ErrAssociationChanged)) {
			n, parseErr := sctp.ParseNotification(b.notification[:r.N])
			if parseErr != nil {
				return nil, info, parseErr
			}
			// A valid RESTART or partial-abort fact must be handled BEFORE the
			// accompanying error/CloseRequired terminates this connection generation.
			b.onNotification(n)
		}
		if err != nil {
			// Includes oversized notifications: retain the fixed allocation policy
			// and close, rather than growing from an untrusted length indefinitely.
			return nil, info, err
		}
		if r.CloseRequired {
			return nil, info, sctp.ErrReadUnusable
		}
		switch r.Kind {
		case sctp.ReadNotification:
			notifications++
			if notifications > 64 {
				return nil, info, fmt.Errorf("notification budget exceeded")
			}
			// Keep the original ctx and partial message; do not refresh the budget.
		case sctp.ReadData:
			if !r.InfoPresent || r.N < 1 || r.N > len(b.data) {
				return nil, info, sctp.ErrMessageBoundary
			}
			if r.N > cap(b.message)-len(b.message) {
				return nil, info, fmt.Errorf("message exceeds %d-byte application limit", cap(b.message))
			}
			if len(b.message) == 0 {
				info = r.Info
			}
			// Upstream checks association/stream/PPID/ordering/SSN continuity.
			b.message = append(b.message, b.data[:r.N]...)
			if r.EndOfRecord() {
				return b.message, info, nil
			}
		case sctp.ReadEOF:
			return nil, info, io.EOF
		default:
			return nil, info, sctp.ErrMessageBoundary
		}
	}
}
