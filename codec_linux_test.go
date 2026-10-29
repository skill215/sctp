//go:build linux && !386

package sctp

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
)

func sndRcvControlMessage(payload []byte) []byte {
	buf := make([]byte, syscall.CmsgSpace(len(payload)))
	header := syscall.Cmsghdr{Level: syscall.IPPROTO_SCTP, Type: SCTP_CMSG_SNDRCV}
	header.SetLen(syscall.CmsgLen(len(payload)))
	copy(buf, toBuf(&header))
	copy(buf[syscall.CmsgLen(0):], payload)
	return buf
}

func TestParseSndRcvInfoOwnsData(t *testing.T) {
	const ppid = uint32(0x01020304)
	info := SndRcvInfo{Stream: 7, PPID: htonl(ppid)}
	message := sndRcvControlMessage(toBuf(&info))
	before := append([]byte(nil), message...)
	decoded, err := parseSndRcvInfo(message)
	if err != nil {
		t.Fatal(err)
	}
	if decoded == nil || decoded.Stream != 7 || decoded.PPID != ppid {
		t.Fatalf("received metadata=%+v", decoded)
	}
	if !bytes.Equal(message, before) {
		t.Fatal("parsing changed the ancillary buffer")
	}
	decoded.Stream = 42
	if !bytes.Equal(message, before) {
		t.Fatal("returned metadata aliases the ancillary buffer")
	}
}

func TestParseSndRcvInfoTruncated(t *testing.T) {
	for size := range int(sndRcvInfoSize) {
		message := sndRcvControlMessage(make([]byte, size))
		if info, err := parseSndRcvInfo(message); info != nil || !errors.Is(err, syscall.EINVAL) {
			t.Fatalf("%d-byte ancillary data: info=%+v, err=%v", size, info, err)
		}
	}
}
