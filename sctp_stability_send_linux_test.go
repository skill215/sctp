//go:build linux && !386

package sctp

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func TestStabilityEmptySends(t *testing.T) {
	for _, mode := range []string{"plain", "metadata", "wrapped"} {
		t.Run(mode, func(t *testing.T) {
			c, p := lifecyclePair(t)
			w := NewSCTPSndRcvInfoWrappedConn(c)
			info := SndRcvInfo{PPID: 42, Stream: 1}
			original := info
			var n int
			var err error
			switch mode {
			case "plain":
				n, err = c.Write(nil)
			case "metadata":
				n, err = c.SCTPWrite(nil, &info)
			case "wrapped":
				n, err = w.Write(toBuf(&info))
			}
			if n != 0 || !errors.Is(err, syscall.EINVAL) {
				t.Fatalf("empty send=%d,%v", n, err)
			}
			if info != original {
				t.Fatal("metadata mutated")
			}
			marker := []byte("no injected prefix")
			if n, err := c.SCTPWrite(marker, &info); err != nil || n != len(marker) {
				t.Fatalf("retry=%d,%v", n, err)
			}
			b := make([]byte, 128)
			n, err = p.Read(b)
			if err != nil || !bytes.Equal(b[:n], marker) {
				t.Fatalf("peer=%q,%v", b[:max(0, n)], err)
			}
		})
	}
}

func stabilityEndpoint(t *testing.T) (*SCTPConn, *SCTPConn, int) {
	t.Helper()
	ln := lifecycleListener(t)
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 132)
	if err != nil {
		t.Fatal(err)
	}
	id, err := SCTPConnect(fd, ln.Addr().(*SCTPAddr))
	if err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	c := NewSCTPConn(fd, nil)
	t.Cleanup(func() { c.Close() })
	p, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	raw, _ := p.SyscallConn()
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	return c, p, id
}

func TestStabilitySendControls(t *testing.T) {
	if SCTP_EOF != 0x200 {
		t.Fatalf("EOF=%#x", SCTP_EOF)
	}
	for _, mode := range []string{"eof", "abort", "reason", "wrapped-eof"} {
		t.Run(mode, func(t *testing.T) {
			c, p, id := stabilityEndpoint(t)
			info := SndRcvInfo{AssocID: int32(id), Flags: SCTP_ABORT}
			if mode == "eof" || mode == "wrapped-eof" {
				info.Flags = SCTP_EOF
			}
			var b []byte
			if mode == "reason" {
				b = []byte("abort reason")
			}
			original := info
			var n int
			var err error
			if mode == "wrapped-eof" {
				n, err = NewSCTPSndRcvInfoWrappedConn(c).Write(toBuf(&info))
				if n != 32 {
					t.Fatalf("control frame count=%d,%v", n, err)
				}
				n -= 32
			} else {
				n, err = c.SCTPWrite(b, &info)
			}
			if err != nil || n != 0 {
				t.Fatalf("control send=%d,%v", n, err)
			}
			if info != original {
				t.Fatal("control metadata mutated")
			}
			n, err = p.Read(make([]byte, 128))
			expected := error(syscall.ECONNRESET)
			if info.Flags == SCTP_EOF {
				expected = io.EOF
			}
			if n != 0 || !errors.Is(err, expected) {
				t.Fatalf("control peer=%d,%v want %v", n, err, expected)
			}
		})
	}
	c, p := lifecyclePair(t)
	if n, err := c.SCTPWrite(nil, &SndRcvInfo{Flags: SCTP_EOF}); n != 0 || !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("inapplicable EOF=%d,%v", n, err)
	}
	if n, err := c.Write([]byte("alive")); n != 5 || err != nil {
		t.Fatalf("after rejected control=%d,%v", n, err)
	}
	if n, err := p.Read(make([]byte, 64)); n != 5 || err != nil {
		t.Fatalf("control affected live peer=%d,%v", n, err)
	}
}

func TestStabilityDefaultSendControls(t *testing.T) {
	c, p := lifecyclePair(t)
	rawControl, rawPeer := lifecyclePair(t)
	for _, conn := range []*SCTPConn{c, rawControl} {
		if err := conn.SetDefaultSentParam(&SndRcvInfo{Flags: SCTP_ABORT}); err != nil {
			t.Fatal(err)
		}
	}
	var rawErr error
	if err := rawControl.withFD(func(fd int) error { _, rawErr = sendmsgZero(fd, nil); return nil }); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Write(nil); n != 0 || !errors.Is(err, rawErr) {
		t.Fatalf("default empty=%d,%v raw=%v", n, err, rawErr)
	}
	if rawErr == nil {
		return
	} // Kernel accepted a control; no user byte was reported.
	// Defaults can ignore non-defaultable control bits. Compare nonempty sends
	// with a direct syscall rather than requiring an unsupported default ABORT.
	marker := []byte("reason")
	var rawN int
	if err := rawControl.withFD(func(fd int) error { rawN, rawErr = syscall.SendmsgN(fd, marker, nil, nil, 0); return nil }); err != nil {
		t.Fatal(err)
	}
	n, err := c.Write(marker)
	if n != max(rawN, 0) || !errors.Is(err, rawErr) {
		t.Fatalf("default send=%d,%v raw=%d,%v", n, err, rawN, rawErr)
	}
	for _, peer := range []*SCTPConn{p, rawPeer} {
		b := make([]byte, 64)
		got, recvErr := peer.Read(b)
		if rawN > 0 {
			if recvErr != nil || !bytes.Equal(b[:got], marker) {
				t.Fatalf("default data=%q,%v", b[:max(0, got)], recvErr)
			}
		} else if got != 0 || !errors.Is(recvErr, syscall.ECONNRESET) {
			t.Fatalf("default control peer=%d,%v", got, recvErr)
		}
	}
}
