package sctp

import "net/netip"

// NotificationKind identifies the populated value in Notification. Unknown
// notifications retain their original Header and borrowed Tail.
type NotificationKind uint8

const (
	NotificationUnknown NotificationKind = iota
	NotificationAssociationChange
	NotificationPeerAddressChange
	NotificationSendFailure
	NotificationShutdown
	NotificationPartialDelivery
)

// Notification contains owned fixed fields and one explicitly borrowed Tail.
// Tail is association info, failed payload, or an unknown notification body.
// It is valid only while the input to ParseNotification remains unchanged.
// The parser neither retains the input elsewhere nor interprets Diameter data.
type Notification struct {
	Header          NotificationHeader
	Kind            NotificationKind
	Association     AssociationChange
	PeerAddress     PeerAddressChange
	SendFailure     SendFailure
	Shutdown        ShutdownNotification
	PartialDelivery PartialDeliveryNotification
	Tail            []byte
}

type AssociationChange struct {
	State                           SCTPState // unknown numeric states are preserved
	Error                           uint16
	OutboundStreams, InboundStreams uint16
	AssocID                         SCTPAssocID
}

// NotificationAddress preserves the Linux sockaddr family, network port and
// IPv6 scope/flow fields. IP is an owned value; no zone/DNS lookup is performed.
type NotificationAddress struct {
	Family            uint16
	Port              uint16
	IP                netip.Addr
	ScopeID, FlowInfo uint32
}
type PeerAddressChange struct {
	Address      NotificationAddress
	State, Error int32 // unknown numeric states/errors are preserved
	AssocID      SCTPAssocID
}

type SendFailure struct {
	Error   uint32
	Info    SndRcvInfo // PPID in host byte order, including valid zero values
	AssocID SCTPAssocID
	// DATA_UNSENT/DATA_SENT is preserved in Notification.Header.Flags.
}

type ShutdownNotification struct{ AssocID SCTPAssocID }
type PartialDeliveryNotification struct {
	Indication                     uint32
	AssocID                        SCTPAssocID
	Stream, Sequence               uint32
	StreamPresent, SequencePresent bool
}

const (
	SendFailureDataUnsent  uint16 = 0
	SendFailureDataSent    uint16 = 1
	PartialDeliveryAborted uint32 = 0
)
