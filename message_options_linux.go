//go:build linux && !386

package sctp

import (
	"syscall"
	"unsafe"
)

const messageFragmentInterleave = 18

// MessageIORequirements is a layout bound, not a kernel probe or total connection
// allocation. Payload/OOB buffers and poller/context state are owned separately.
func MessageIORequirements(m MessageOptions) (IORequirements, error) {
	if e := validateMessageOptions(m); e != nil {
		return IORequirements{}, e
	}
	n := 8
	if m.Events&SCTP_EVENT_ASSOCIATION != 0 {
		n = max(n, 20)
	}
	if m.Events&SCTP_EVENT_ADDRESS != 0 {
		n = max(n, 148)
	}
	if m.Events&SCTP_EVENT_SEND_FAILURE != 0 {
		n = max(n, 48)
	}
	if m.Events&SCTP_EVENT_SHUTDOWN != 0 {
		n = max(n, 12)
	}
	if m.Events&SCTP_EVENT_PARTIAL_DELIVERY != 0 {
		n = max(n, 24)
	}
	// Conservative logical workspace bound: result/metadata copies, recv/send
	// descriptors and scalar workspace. Excludes Go call frames, errors, closures,
	// context hooks/timers and runtime poller state (connection budget evidence).
	readScratch := 3*unsafe.Sizeof(ReadResult{}) + 2*unsafe.Sizeof(SndRcvInfo{}) + unsafe.Sizeof(syscall.Msghdr{}) + unsafe.Sizeof(syscall.Iovec{}) + 128
	writeScratch := 64 + unsafe.Sizeof(SndInfo{}) + unsafe.Sizeof(syscall.Msghdr{}) + unsafe.Sizeof(syscall.Iovec{}) + 128
	return IORequirements{OOBMin: syscall.CmsgSpace(32), NotificationMin: n, PeekScratchBytes: 8, ReadScratchBytes: uint64(readScratch), WriteScratchBytes: uint64(writeScratch)}, nil
}
func configureMessageFD(fd int, m MessageOptions) error {
	var events [10]byte
	events[0] = 1
	for i := 1; i < len(events); i++ {
		if m.Events&(1<<i) != 0 {
			events[i] = 1
		}
	}
	if _, _, e := setsockopt(fd, SCTP_EVENTS, uintptr(unsafe.Pointer(&events[0])), uintptr(len(events))); e != nil {
		return e
	}
	for _, o := range [][2]int{{SCTP_NODELAY, boolint(m.NoDelay)}, {messageFragmentInterleave, 0}, {SCTP_DISABLE_FRAGMENTS, boolint(!m.AllowFragmentation)}} {
		if e := syscall.SetsockoptInt(fd, SOL_SCTP, o[0], o[1]); e != nil {
			return e
		}
	}
	return verifyMessageFD(fd, m)
}
func readMessageOptionsFD(fd int) (MessageOptionState, error) {
	var events [10]byte
	size := uint32(len(events))
	if _, _, e := getsockopt(fd, SCTP_EVENTS, uintptr(unsafe.Pointer(&events[0])), &size); e != nil {
		return MessageOptionState{}, e
	}
	if size < 7 || size > uint32(len(events)) {
		return MessageOptionState{}, ErrMessageBoundary
	}
	m := MessageOptions{}
	if events[0] == 1 {
		m.ReceiveInfo = ReceiveInfoSNDRCV
	} else if events[0] != 0 {
		return MessageOptionState{}, ErrMessageBoundary
	}
	for i := 1; i < int(size); i++ {
		if events[i] > 1 {
			return MessageOptionState{}, ErrMessageBoundary
		}
		if events[i] != 0 {
			m.Events |= 1 << i
		}
	}
	no, e := syscall.GetsockoptInt(fd, SOL_SCTP, SCTP_NODELAY)
	if e != nil {
		return MessageOptionState{}, e
	}
	level, e := syscall.GetsockoptInt(fd, SOL_SCTP, messageFragmentInterleave)
	if e != nil {
		return MessageOptionState{}, e
	}
	dis, e := syscall.GetsockoptInt(fd, SOL_SCTP, SCTP_DISABLE_FRAGMENTS)
	if e != nil {
		return MessageOptionState{}, e
	}
	if no < 0 || no > 1 || dis < 0 || dis > 1 || level < 0 || level > 255 {
		return MessageOptionState{}, ErrMessageBoundary
	}
	m.NoDelay = no == 1
	m.AllowFragmentation = dis == 0
	m.FragmentInterleave = uint8(level)
	return MessageOptionState{Options: m, Layout: "linux-sndrcv-events-send-failed-v1"}, nil
}
func verifyMessageFD(fd int, m MessageOptions) error {
	actual, e := readMessageOptionsFD(fd)
	if e != nil {
		return e
	}
	if actual.Options != m {
		return ErrInvalidConfig
	}
	return nil
}
func (c *SCTPConn) GetMessageOptions() (MessageOptionState, error) {
	if c == nil || c.messages == nil {
		return MessageOptionState{}, ErrIOMode
	}
	var state MessageOptionState
	e := c.withFD(func(fd int) error { var e error; state, e = readMessageOptionsFD(fd); return e })
	return state, e
}
func finishMessageConn(c *SCTPConn, m *MessageOptions) error {
	if m == nil {
		return nil
	}
	if e := c.withFD(func(fd int) error { return verifyMessageFD(fd, *m) }); e != nil {
		return e
	}
	status, e := c.GetStatus()
	if e != nil {
		return e
	}
	if status.Instreams == 0 || status.Ostreams == 0 {
		return ErrInvalidConfig
	}
	c.messageIn, c.messageOut = status.Instreams, status.Ostreams
	c.messages = m
	c.messageAssoc = int32(status.AssocID)
	return nil
}
