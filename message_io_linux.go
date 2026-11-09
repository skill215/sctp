//go:build linux && !386

package sctp

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

func overlapMessageBuffers(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	x := uintptr(unsafe.Pointer(unsafe.SliceData(a)))
	y := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	return x < y+uintptr(len(b)) && y < x+uintptr(len(a))
}

// No source-address allocation: one-to-one metadata comes from SNDRCV.
func recvMessage(fd int, data, oob []byte, flags int) (int, int, int, error) {
	var v syscall.Iovec
	if len(data) > 0 {
		v.Base = &data[0]
		v.SetLen(len(data))
	}
	var msg syscall.Msghdr
	msg.Iov = &v
	msg.Iovlen = 1
	if len(oob) > 0 {
		msg.Control = &oob[0]
		msg.SetControllen(len(oob))
	}
	n, _, errno := syscall.Syscall(syscall.SYS_RECVMSG, uintptr(fd), uintptr(unsafe.Pointer(&msg)), uintptr(flags))
	runtime.KeepAlive(data)
	runtime.KeepAlive(oob)
	if errno != 0 {
		return 0, 0, 0, errno
	}
	return int(n), int(msg.Controllen), int(msg.Flags), nil
}
func messageInfo(oob []byte) (info SndRcvInfo, present bool, err error) {
	hs := syscall.CmsgLen(0)
	word := int(unsafe.Sizeof(uintptr(0)))
	for len(oob) > 0 {
		if len(oob) < hs {
			return info, false, ErrMessageBoundary
		}
		var length uint64
		if word == 8 {
			length = nativeEndian.Uint64(oob)
		} else {
			length = uint64(nativeEndian.Uint32(oob))
		}
		if length < uint64(hs) || length > uint64(len(oob)) {
			return info, false, ErrMessageBoundary
		}
		level := int32(nativeEndian.Uint32(oob[word:]))
		kind := int32(nativeEndian.Uint32(oob[word+4:]))
		if level == SOL_SCTP && kind == SCTP_CMSG_SNDRCV {
			if present || length != uint64(hs+32) {
				return info, false, ErrMessageBoundary
			}
			p := oob[hs:int(length)]
			info = decodeMessageInfo(p)
			present = true
		}
		step := syscall.CmsgSpace(int(length) - hs)
		if step > len(oob) {
			if int(length) == len(oob) {
				break
			}
			return info, false, ErrMessageBoundary
		}
		oob = oob[step:]
	}
	return info, present, nil
}
func messageSendControl(out []byte, info SndInfo) []byte {
	hs := syscall.CmsgLen(0)
	out = out[:syscall.CmsgSpace(32)]
	clear(out)
	word := int(unsafe.Sizeof(uintptr(0)))
	if word == 8 {
		nativeEndian.PutUint64(out, uint64(syscall.CmsgLen(32)))
	} else {
		nativeEndian.PutUint32(out, uint32(syscall.CmsgLen(32)))
	}
	nativeEndian.PutUint32(out[word:], SOL_SCTP)
	nativeEndian.PutUint32(out[word+4:], SCTP_CMSG_SNDRCV)
	p := out[hs:]
	nativeEndian.PutUint16(p, info.SID)
	nativeEndian.PutUint16(p[4:], info.Flags)
	binary.BigEndian.PutUint32(p[8:], info.PPID)
	nativeEndian.PutUint32(p[12:], info.Context)
	nativeEndian.PutUint32(p[28:], uint32(info.AssocID))
	return out
}
func (s *socketIO) messageAdmission(read bool) error {
	s.deadlineMu.Lock()
	defer s.deadlineMu.Unlock()
	d := s.direction(read)
	if d.owner == nil {
		return ErrIOMode
	}
	return s.reasonLocked(d, d.owner, time.Now())
}

// ReadEvent returns one data fragment or raw notification in caller-owned buffers.
// MSG_EOR ends a message. InfoPresent distinguishes missing SNDRCV from zero values.
// A CloseRequired result permanently disables reads; the caller must close the connection.
func (c *SCTPConn) ReadEvent(ctx context.Context, data, notification, oob []byte) (ReadResult, error) {
	return c.readEvent(ctx, data, notification, oob, recvMessage)
}

func (c *SCTPConn) readEvent(ctx context.Context, data, notification, oob []byte, recv func(int, []byte, []byte, int) (int, int, int, error)) (ReadResult, error) {
	var result ReadResult
	if c == nil || c.messages == nil {
		return result, ErrIOMode
	}
	if e := contextError(ctx); e != nil {
		return result, e
	}
	req, e := MessageIORequirements(*c.messages)
	if e != nil {
		return result, e
	}
	if len(data) < 1 || len(notification) < req.NotificationMin || len(oob) < req.OOBMin {
		return result, ErrShortBuffer
	}
	if overlapMessageBuffers(data, notification) || overlapMessageBuffers(data, oob) || overlapMessageBuffers(notification, oob) {
		return result, ErrInvalidConfig
	}
	owner, e := c.socketRaw()
	if e != nil {
		return result, e
	}
	timeout, e := owner.deadline(true)
	if e != nil {
		return result, e
	}
	var actual error
	e = owner.pollContext(ctx, true, timeout, func(fd uintptr) bool {
		if c.messageReadErr != nil {
			result.CloseRequired = true
			actual = errors.Join(ErrReadUnusable, c.messageReadErr)
			return true
		}
		var peek [8]byte
		n, _, flags, err := recv(int(fd), peek[:], nil, syscall.MSG_PEEK)
		if err == syscall.EAGAIN || err == syscall.EINTR {
			return false
		}
		if err != nil {
			actual = err
			if c.messageCursor.active {
				result.CloseRequired = true
				actual = errors.Join(actual, ErrMessageBoundary)
				c.messageReadErr = actual
			}
			return true
		}
		notify := flags&MSG_NOTIFICATION != 0
		if n == 0 && !notify && flags&syscall.MSG_EOR == 0 && flags&syscall.MSG_CTRUNC == 0 {
			result.Kind = ReadEOF
			actual = io.EOF
			if e := c.advanceMessage(result); e != nil {
				result.CloseRequired = true
				actual = errors.Join(actual, e)
				c.messageReadErr = actual
			}
			return true
		}
		var declared uint32
		if notify {
			declared = nativeEndian.Uint32(peek[4:])
			if n != 8 || declared < 8 {
				result.Kind = ReadNotification
				result.CloseRequired = true
				actual = ErrMessageBoundary
				c.messageReadErr = actual
				return true
			}
			if uint64(declared) > uint64(len(notification)) {
				result.Kind = ReadNotification
				result.RequiredNotificationBytes = declared
				actual = ErrNotificationTooLarge
				return true
			}
		}
		// Peek was not consumption. Cancellation/deadline/Close can still forbid recv.
		if actual = owner.messageAdmission(true); actual != nil {
			return true
		}
		target := data
		if notify {
			target = notification
		}
		n, on, flags, err := recv(int(fd), target, oob, 0)
		if (err == syscall.EAGAIN || err == syscall.EINTR) && n <= 0 && on <= 0 {
			return false
		}
		result, actual = messageReceived(notify, declared, target, oob, n, on, flags, err)
		// A terminal consume failure can follow a successful peek without new
		// bytes. Earlier fragments still lost their boundary; do not let the
		// zero-progress result leave that unfinished message readable.
		if actual != nil && c.messageCursor.active && !result.CloseRequired {
			result.CloseRequired = true
			actual = errors.Join(actual, ErrMessageBoundary)
		}
		if notify && n >= 8 && n <= len(target) && nativeEndian.Uint32(peek[:4]) != nativeEndian.Uint32(target[:4]) {
			result.CloseRequired = true
			actual = errors.Join(actual, ErrMessageBoundary)
		}
		if !result.CloseRequired {
			if e := c.advanceMessage(result); e != nil {
				result.CloseRequired = true
				actual = errors.Join(actual, e)
			}
		}
		// A restart ends the lifetime of the establishment stream snapshot. Preserve
		// the raw notification so callers can still decode all restart facts.
		if notify && result.Consumed && result.N >= 20 && nativeEndian.Uint16(target) == uint16(SCTP_ASSOC_CHANGE) && nativeEndian.Uint16(target[8:]) == uint16(SCTP_RESTART) {
			c.messageRestart.Store(true)
			result.CloseRequired = true
			actual = errors.Join(actual, ErrAssociationChanged)
		}
		// Keep the notification a successful raw event, but an explicitly
		// aborted partial delivery cannot be resumed as the next message.
		// In particular, unordered SSN is not a substitute message identity.
		if notify && result.Consumed && result.N == 24 && c.messageCursor.active && nativeEndian.Uint16(target) == uint16(SCTP_PARTIAL_DELIVERY_EVENT) && nativeEndian.Uint32(target[8:]) == PartialDeliveryAborted {
			result.CloseRequired = true
		}
		if result.CloseRequired {
			c.messageReadErr = actual
			if c.messageReadErr == nil {
				c.messageReadErr = ErrMessageBoundary
			}
		}
		return true
	})
	if e != nil {
		return result, e
	}
	return result, actual
}

// Pure post-consumption validation preserves observable bytes/flags on errors.
func messageReceived(notify bool, declared uint32, target, oob []byte, n, on, flags int, actual error) (ReadResult, error) {
	r := ReadResult{N: max(0, min(n, len(target))), OOBN: max(0, min(on, len(oob))), RecvFlags: flags}
	if n <= 0 && on <= 0 && actual != nil {
		return r, actual
	}
	r.Kind = ReadData
	if notify {
		r.Kind = ReadNotification
	}
	r.Consumed = true
	bad := n < 0 || n > len(target) || on < 0 || on > len(oob) || flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0 || (flags&MSG_NOTIFICATION != 0) != notify
	if notify {
		bad = bad || n < 8 || uint64(n) != uint64(declared) || flags&syscall.MSG_EOR == 0
		if n >= 8 && n <= len(target) {
			bad = bad || nativeEndian.Uint32(target[4:]) != declared
		}
	} else {
		var e error
		r.Info, r.InfoPresent, e = messageInfo(oob[:r.OOBN])
		bad = bad || e != nil
		if n == 0 && on == 0 && flags&syscall.MSG_EOR == 0 && actual == nil && !bad {
			r.Kind = ReadEOF
			r.Consumed = false
			return r, io.EOF
		}
	}
	if bad || actual != nil {
		r.CloseRequired = true
		return r, errors.Join(actual, ErrMessageBoundary)
	}
	return r, nil
}

// WriteMsgContext sends one message with explicit SNDRCV metadata and zero TTL.
// A partial success returns io.ErrShortWrite; no remaining bytes are resubmitted.
func (c *SCTPConn) WriteMsgContext(ctx context.Context, p []byte, info SndInfo) (int, error) {
	return c.writeMsgContext(ctx, p, info, syscall.SendmsgN)
}

func (c *SCTPConn) writeMsgContext(ctx context.Context, p []byte, info SndInfo, send func(int, []byte, []byte, syscall.Sockaddr, int) (int, error)) (int, error) {
	if c == nil || c.messages == nil {
		return 0, ErrIOMode
	}
	if e := contextError(ctx); e != nil {
		return 0, e
	}
	if c.messageRestart.Load() {
		return 0, ErrAssociationChanged
	}
	if len(p) == 0 || info.SID >= c.messageOut || info.Flags & ^uint16(SCTP_UNORDERED) != 0 || info.AssocID != 0 && info.AssocID != c.messageAssoc {
		return 0, ErrInvalidConfig
	}
	owner, e := c.socketRaw()
	if e != nil {
		return 0, e
	}
	timeout, e := owner.deadline(false)
	if e != nil {
		return 0, e
	}
	var control [64]byte
	oob := messageSendControl(control[:], info)
	var n int
	var actual error
	e = owner.pollContext(ctx, false, timeout, func(fd uintptr) bool {
		if c.messageRestart.Load() {
			actual = ErrAssociationChanged
			return true
		}
		n, actual = send(int(fd), p, oob, nil, 0)
		return n > 0 || actual != syscall.EAGAIN && actual != syscall.EINTR
	})
	if e != nil {
		return max(n, 0), e
	}
	if actual == nil && n != len(p) {
		actual = io.ErrShortWrite
	}
	return max(n, 0), actual
}

// decodeMessageInfo requires a validated 32-byte SNDRCV payload.
func decodeMessageInfo(p []byte) SndRcvInfo {
	return SndRcvInfo{Stream: nativeEndian.Uint16(p), SSN: nativeEndian.Uint16(p[2:]), Flags: nativeEndian.Uint16(p[4:]), PPID: binary.BigEndian.Uint32(p[8:]), Context: nativeEndian.Uint32(p[12:]), TTL: nativeEndian.Uint32(p[16:]), TSN: nativeEndian.Uint32(p[20:]), CumTSN: nativeEndian.Uint32(p[24:]), AssocID: int32(nativeEndian.Uint32(p[28:]))}
}
