package sctp

import "errors"

type ReceiveInfoMode uint8

const (
	ReceiveInfoUnspecified ReceiveInfoMode = iota
	ReceiveInfoSNDRCV
)

// MessageOptions is copied before fd creation. Only SNDRCV and level0 are
// supported; Events is a subset of the five existing SCTP_EVENT_* bits below.
type MessageOptions struct {
	ReceiveInfo        ReceiveInfoMode
	Events             uint32
	NoDelay            bool
	FragmentInterleave uint8
	AllowFragmentation bool
}
type IORequirements struct {
	OOBMin, NotificationMin                               int
	PeekScratchBytes, ReadScratchBytes, WriteScratchBytes uint64
}
type MessageOptionState struct {
	Options MessageOptions
	// Layout identifies the fixed Linux SNDRCV/EVENTS/old SEND_FAILED ABI.
	Layout string
}
type ReadKind uint8

const (
	ReadNone ReadKind = iota
	ReadData
	ReadNotification
	ReadEOF
)

type ReadResult struct {
	Kind                                 ReadKind
	N, OOBN, RecvFlags                   int
	Info                                 SndRcvInfo
	InfoPresent, Consumed, CloseRequired bool
	RequiredNotificationBytes            uint32
}

var (
	ErrIOMode               = errors.New("SCTP I/O mode mismatch")
	ErrShortBuffer          = errors.New("SCTP message buffers too small")
	ErrNotificationTooLarge = errors.New("SCTP notification exceeds supplied buffer")
	ErrMessageBoundary      = errors.New("invalid SCTP message boundary or metadata")
	ErrReadUnusable         = errors.New("SCTP message read boundary unusable; close required")
)

const messageEventMask = SCTP_EVENT_ASSOCIATION | SCTP_EVENT_ADDRESS | SCTP_EVENT_SEND_FAILURE | SCTP_EVENT_SHUTDOWN | SCTP_EVENT_PARTIAL_DELIVERY

func validateMessageOptions(m MessageOptions) error {
	if m.ReceiveInfo != ReceiveInfoSNDRCV || m.FragmentInterleave != 0 || m.Events & ^uint32(messageEventMask) != 0 {
		return ErrInvalidConfig
	}
	return nil
}
func copyMessageConfig(cfg *SocketConfig) (*MessageOptions, error) {
	if cfg == nil {
		return nil, ErrInvalidConfig
	}
	if cfg.Messages == nil {
		return nil, nil
	}
	if cfg.Control != nil || cfg.NotificationHandler != nil {
		return nil, ErrInvalidConfig
	}
	m := *cfg.Messages
	if e := validateMessageOptions(m); e != nil {
		return nil, e
	}
	return &m, nil
}

// ErrAssociationChanged means a received restart invalidated the negotiated
// stream snapshot. The notification's raw bytes/flags remain available; close
// this connection generation. Dynamic stream reset/expansion is not supported.
var ErrAssociationChanged = errors.New("SCTP association restarted; close required")

// EndOfRecord reports the native EOR flag portably; RecvFlags remains available.
// It is evidence about this receive record, not application frame completeness.
func (r ReadResult) EndOfRecord() bool { return r.RecvFlags&0x80 != 0 }

type messageCursor struct {
	info   SndRcvInfo
	active bool
}

// advance runs only under the read gate. Notifications and unconsumed
// cancellation leave the cursor untouched. No payload or caller buffers survive.
func (c *SCTPConn) advanceMessage(r ReadResult) error {
	if r.Kind == ReadEOF && c.messageCursor.active {
		return ErrMessageBoundary
	}
	if r.Kind != ReadData || !r.Consumed {
		return nil
	}
	i := r.Info
	if !r.InfoPresent || i.AssocID != c.messageAssoc || i.Stream >= c.messageIn {
		return ErrMessageBoundary
	}
	old := c.messageCursor.info
	if c.messageCursor.active && (i.AssocID != old.AssocID || i.Stream != old.Stream || i.PPID != old.PPID || i.Flags&SCTP_UNORDERED != old.Flags&SCTP_UNORDERED || i.Flags&SCTP_UNORDERED == 0 && i.SSN != old.SSN) {
		return ErrMessageBoundary
	}
	if r.EndOfRecord() {
		c.messageCursor = messageCursor{}
	} else {
		c.messageCursor = messageCursor{info: i, active: true}
	}
	return nil
}
