//go:build linux && !386

package sctp

import (
	"encoding/binary"
	"net/netip"
	"syscall"
)

// ParseNotification decodes one complete notification in the selected Linux
// SNDRCV/EVENTS/old-SEND_FAILED ABI. Length must exactly match len(p).
// Fixed structures are copied; only Tail borrows p, with capacity limited to
// its length. Unknown states/flags remain numeric, unknown types remain raw.
// Malformed input returns a zero Notification and ErrMessageBoundary.
func ParseNotification(p []byte) (Notification, error) {
	var n Notification
	if len(p) < 8 {
		return n, ErrMessageBoundary
	}
	h := NotificationHeader{Type: nativeEndian.Uint16(p), Flags: nativeEndian.Uint16(p[2:]), Length: nativeEndian.Uint32(p[4:])}
	if uint64(h.Length) != uint64(len(p)) || h.Type <= uint16(SCTP_SN_TYPE_BASE) {
		return n, ErrMessageBoundary
	}
	n.Header = h
	switch SCTPNotificationType(h.Type) {
	case SCTP_ASSOC_CHANGE:
		if len(p) < 20 {
			return Notification{}, ErrMessageBoundary
		}
		n.Kind = NotificationAssociationChange
		n.Association = AssociationChange{State: SCTPState(nativeEndian.Uint16(p[8:])), Error: nativeEndian.Uint16(p[10:]), OutboundStreams: nativeEndian.Uint16(p[12:]), InboundStreams: nativeEndian.Uint16(p[14:]), AssocID: SCTPAssocID(nativeEndian.Uint32(p[16:]))}
		if len(p) > 20 {
			n.Tail = p[20:len(p):len(p)]
		}
	case SCTP_PEER_ADDR_CHANGE:
		if len(p) != 148 {
			return Notification{}, ErrMessageBoundary
		}
		a, e := notificationAddress(p[8:136])
		if e != nil {
			return Notification{}, e
		}
		n.Kind = NotificationPeerAddressChange
		n.PeerAddress = PeerAddressChange{Address: a, State: int32(nativeEndian.Uint32(p[136:])), Error: int32(nativeEndian.Uint32(p[140:])), AssocID: SCTPAssocID(nativeEndian.Uint32(p[144:]))}
	case SCTP_SEND_FAILED:
		if len(p) < 48 {
			return Notification{}, ErrMessageBoundary
		}
		n.Kind = NotificationSendFailure
		n.SendFailure = SendFailure{Error: nativeEndian.Uint32(p[8:]), Info: decodeMessageInfo(p[12:44]), AssocID: SCTPAssocID(nativeEndian.Uint32(p[44:]))}
		if len(p) > 48 {
			n.Tail = p[48:len(p):len(p)]
		}
	case SCTP_SHUTDOWN_EVENT:
		if len(p) != 12 {
			return Notification{}, ErrMessageBoundary
		}
		n.Kind = NotificationShutdown
		n.Shutdown.AssocID = SCTPAssocID(nativeEndian.Uint32(p[8:]))
	case SCTP_PARTIAL_DELIVERY_EVENT:
		// The selected current Linux ABI is exactly24 bytes. A truncated/older
		// prefix must not manufacture stream0/sequence0 presence.
		if len(p) != 24 {
			return Notification{}, ErrMessageBoundary
		}
		n.Kind = NotificationPartialDelivery
		n.PartialDelivery = PartialDeliveryNotification{Indication: nativeEndian.Uint32(p[8:]), AssocID: SCTPAssocID(nativeEndian.Uint32(p[12:])), Stream: nativeEndian.Uint32(p[16:]), Sequence: nativeEndian.Uint32(p[20:]), StreamPresent: true, SequencePresent: true}
	default:
		n.Kind = NotificationUnknown
		if len(p) > 8 {
			n.Tail = p[8:len(p):len(p)]
		}
	}
	return n, nil
}

func notificationAddress(p []byte) (NotificationAddress, error) {
	a := NotificationAddress{Family: nativeEndian.Uint16(p), Port: binary.BigEndian.Uint16(p[2:])}
	switch a.Family {
	case syscall.AF_INET:
		a.IP = netip.AddrFrom4([4]byte(p[4:8]))
	case syscall.AF_INET6:
		a.FlowInfo = binary.BigEndian.Uint32(p[4:])
		a.IP = netip.AddrFrom16([16]byte(p[8:24]))
		a.ScopeID = nativeEndian.Uint32(p[24:])
	default:
		return NotificationAddress{}, ErrMessageBoundary
	}
	return a, nil
}
