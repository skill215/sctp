//go:build linux && !386

package sctp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// This test uses SOL_SOCKET so it also runs under user-mode emulators that
// cannot translate SCTP options. It exercises the syscall path SCTP uses.
func TestSocketOptionLengthABI(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fds[0])
	defer syscall.Close(fds[1])
	const guard = uint32(0xa1b2c3d4)
	length := struct{ size, guard uint32 }{size: 4, guard: guard}
	value := struct {
		value int32
		guard uint32
	}{guard: guard}
	_, _, err = getSocketOption(fds[0], syscall.SOL_SOCKET, syscall.SO_TYPE, uintptr(unsafe.Pointer(&value.value)), uintptr(unsafe.Pointer(&length.size)))
	if err != nil {
		t.Fatal(err)
	}
	if value.value != syscall.SOCK_STREAM || length.size != 4 || length.guard != guard || value.guard != guard {
		t.Fatalf("getsockopt value=%+v length=%+v", value, length)
	}
}

func TestSocketTimeoutABI(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	client, peer := NewSCTPConn(fds[0], nil), NewSCTPConn(fds[1], nil)
	defer client.Close()
	defer peer.Close()
	raw, err := client.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, testSocketTimeout); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	start := time.Now()
	if _, err := client.Read(buf); !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("Read timeout=%v", err)
	}
	if elapsed := time.Since(start); elapsed < testSocketTimeout/2 || elapsed > time.Second {
		t.Fatalf("timeout elapsed=%v", elapsed)
	}
	if _, err := peer.Write([]byte("resumed")); err != nil {
		t.Fatal(err)
	}
	if n, err := client.Read(buf); err != nil || string(buf[:n]) != "resumed" {
		t.Fatalf("Read after timeout=%d,%v", n, err)
	}
}

func TestSCTPPublicGetsockoptLength(t *testing.T) {
	client, _ := lifecyclePair(t)
	if err := client.SubscribeEvents(SCTP_EVENT_DATA_IO | SCTP_EVENT_SENDER_DRY); err != nil {
		t.Fatal(err)
	}
	const guard = uint32(0xa1b2c3d4)
	length := struct{ size, guard uint32 }{size: uint32(unsafe.Sizeof(EventSubscribe{})), guard: guard}
	value := struct {
		events EventSubscribe
		guard  [8]byte
	}{guard: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
	wantGuard := value.guard
	_, _, err := client.Getsockopt(SCTP_EVENTS, uintptr(unsafe.Pointer(&value.events)), uintptr(unsafe.Pointer(&length.size)))
	if err != nil {
		t.Fatal(err)
	}
	if length.size != 10 || length.guard != guard || value.guard != wantGuard || value.events.DataIO != 1 || value.events.SenderDry != 1 {
		t.Fatalf("SCTP_EVENTS value=%+v length=%+v", value, length)
	}
	flags, err := client.SubscribedEvents()
	if err != nil || flags != SCTP_EVENT_DATA_IO|SCTP_EVENT_SENDER_DRY {
		t.Fatalf("SubscribedEvents=%#x,%v", flags, err)
	}
}

func TestWrappedWriteUnalignedHeader(t *testing.T) {
	info := SndRcvInfo{Stream: 123, SSN: 7, PPID: 0x01020304, Context: 0x11223344}
	frame := append(toBuf(&info), []byte("payload")...)
	for offset := range 4 {
		t.Run(strconv.Itoa(offset), func(t *testing.T) {
			storage := make([]byte, offset+len(frame))
			b := storage[offset:]
			copy(b, frame)
			wrapped := &SCTPSndRcvInfoWrappedConn{conn: &SCTPConn{}}
			if n, err := wrapped.Write(b); n != 0 || !errors.Is(err, syscall.EBADF) {
				t.Fatalf("Write on uninitialized connection=%d,%v", n, err)
			}
			if !bytes.Equal(b, frame) {
				t.Fatal("Write changed its input")
			}
		})
	}
}

func TestWrappedWriteUnalignedRoundTrip(t *testing.T) {
	client, server := lifecyclePair(t)
	writer, reader := NewSCTPSndRcvInfoWrappedConn(client), NewSCTPSndRcvInfoWrappedConn(server)
	info := SndRcvInfo{Stream: 1, PPID: 0x01020304}
	frame := append(toBuf(&info), []byte("unaligned payload")...)
	b := make([]byte, len(frame)+1)[1:]
	copy(b, frame)
	if n, err := writer.Write(b); err != nil || n != len(frame) {
		t.Fatalf("Write=%d,%v", n, err)
	}
	if !bytes.Equal(b, frame) {
		t.Fatal("Write changed its input")
	}
	buf := make([]byte, len(frame)+32)[1:]
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var received SndRcvInfo
	if err := binary.Read(bytes.NewReader(buf[:sndRcvInfoSize]), nativeEndian, &received); err != nil {
		t.Fatal(err)
	}
	if n != len(frame) || received.PPID != info.PPID || received.Stream != info.Stream || !bytes.Equal(buf[sndRcvInfoSize:n], frame[sndRcvInfoSize:]) {
		t.Fatalf("read n=%d info=%+v payload=%q", n, received, buf[sndRcvInfoSize:n])
	}
}

func TestWrappedWriteErrorCounts(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprintf("closed=%v", closed), func(t *testing.T) {
			client, _ := lifecyclePair(t)
			wrapped := NewSCTPSndRcvInfoWrappedConn(client)
			if closed {
				client.Close()
			}
			frame := append(toBuf(&SndRcvInfo{Stream: 0xffff}), []byte("invalid stream")...)
			if n, err := wrapped.Write(frame); err == nil || n != 0 {
				t.Fatalf("failed Write=%d,%v", n, err)
			}
		})
	}
}

func TestWrappedWriteTimeoutRetry(t *testing.T) {
	client, server := timeoutPair(t, syscall.SO_SNDTIMEO)
	if err := client.SetWriteBuffer(4096); err != nil {
		t.Fatal(err)
	}
	if err := server.SetReadBuffer(4096); err != nil {
		t.Fatal(err)
	}
	if err := server.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
		t.Fatal(err)
	}
	filled := false
	for range 4096 {
		_, err := client.Write(make([]byte, 1024))
		if errors.Is(err, syscall.EAGAIN) {
			filled = true
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !filled {
		t.Fatal("did not reach backpressure")
	}
	wrapped := NewSCTPSndRcvInfoWrappedConn(client)
	info := SndRcvInfo{PPID: 0x11223344}
	frame := append(toBuf(&info), []byte("retry marker")...)
	if n, err := wrapped.Write(frame); !errors.Is(err, syscall.EAGAIN) || n != 0 {
		t.Fatalf("timed-out Write=%d,%v", n, err)
	}
	raw, _ := client.SyscallConn()
	if err := setSocketTimeout(raw, syscall.SO_SNDTIMEO, 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	if err := server.file.SetReadDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := client.file.SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	done, stopped := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(stopped)
		buf := make([]byte, 2048)
		for {
			n, received, err := server.SCTPRead(buf)
			if err != nil {
				done <- err
				return
			}
			if string(buf[:n]) == "retry marker" {
				if received == nil || received.PPID != info.PPID {
					done <- fmt.Errorf("retry metadata: %+v", received)
				} else {
					done <- nil
				}
				return
			}
		}
	}()
	t.Cleanup(func() { client.Close(); server.Close(); <-stopped })
	if n, err := wrapped.Write(frame); err != nil || n != len(frame) {
		t.Fatalf("retry Write=%d,%v", n, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDefaultSentParamPPID(t *testing.T) {
	client, server := lifecyclePair(t)
	if err := server.SubscribeEvents(SCTP_EVENT_DATA_IO); err != nil {
		t.Fatal(err)
	}
	for _, ppid := range []uint32{0, 1, 0x01020304, 0xffffffff} {
		t.Run(fmt.Sprintf("%08x", ppid), func(t *testing.T) {
			info := SndRcvInfo{Stream: 1, PPID: ppid, Context: 0x11223344, TTL: 1000}
			before := info
			if err := client.SetDefaultSentParam(&info); err != nil {
				t.Fatal(err)
			}
			if info != before {
				t.Fatal("SetDefaultSentParam changed its input")
			}
			got, err := client.GetDefaultSentParam()
			if err != nil {
				t.Fatal(err)
			}
			if got.PPID != ppid || got.Stream != info.Stream || got.Context != info.Context || got.TTL != info.TTL {
				t.Fatalf("default params=%+v, want %+v", got, info)
			}
			if _, err := client.Write([]byte("default PPID")); err != nil {
				t.Fatal(err)
			}
			_, received, err := server.SCTPRead(make([]byte, 128))
			if err != nil {
				t.Fatal(err)
			}
			if received == nil || received.PPID != ppid || received.Stream != info.Stream {
				t.Fatalf("received metadata=%+v", received)
			}
		})
	}
}

func TestRawSockaddrMixedFamilies(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1", "::1"}, {"::1", "127.0.0.1"}, {"127.0.0.1", "127.0.0.2", "::1"}} {
		t.Run(fmt.Sprint(addresses), func(t *testing.T) {
			original := &SCTPAddr{Port: 9000}
			for _, ip := range addresses {
				original.IPAddrs = append(original.IPAddrs, net.IPAddr{IP: net.ParseIP(ip)})
			}
			raw := append([]byte{0}, original.ToRawSockAddrBuf()...)[1:]
			got, err := resolveFromRawAddr(raw, len(addresses))
			if err != nil {
				t.Fatal(err)
			}
			assertAddressIPs(t, got, original)
			// Returned addresses must own their bytes rather than alias the
			// temporary kernel buffer, including after an unaligned decode.
			for i := range raw {
				raw[i] = 0
			}
			assertAddressIPs(t, got, original)
		})
	}
}

func assertAddressIPs(t *testing.T, got, want *SCTPAddr) {
	t.Helper()
	if got == nil || got.Port == 0 || want.Port != 0 && got.Port != want.Port || len(got.IPAddrs) != len(want.IPAddrs) {
		t.Fatalf("address=%v, want %v", got, want)
	}
	for _, expected := range want.IPAddrs {
		found := false
		for _, actual := range got.IPAddrs {
			found = found || actual.IP.Equal(expected.IP)
		}
		if !found {
			t.Errorf("missing %s in %s", expected.IP, got)
		}
	}
}

func TestRawSockaddrInvalidBuffers(t *testing.T) {
	inet6 := (&SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv6loopback}}}).ToRawSockAddrBuf()
	for _, tt := range []struct {
		name  string
		buf   []byte
		count int
	}{
		{"empty", nil, 1}, {"negative", nil, -1}, {"large count", nil, 1 << 30},
		{"short family", []byte{1}, 1}, {"short ipv6", inet6[:len(inet6)-1], 1},
		{"unknown family", make([]byte, 16), 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := resolveFromRawAddr(tt.buf, tt.count); err == nil {
				t.Fatal("malformed sockaddr was accepted")
			}
		})
	}
	if got, err := resolveFromRawAddr(nil, 0); err != nil || len(got.IPAddrs) != 0 {
		t.Fatalf("empty address list=%v,%v", got, err)
	}
}

func TestMixedKernelAddressFamilies(t *testing.T) {
	if !supportsIPv6() {
		t.Skip("IPv6 is unavailable")
	}
	addr, err := ResolveSCTPAddr("sctp", "127.0.0.1/[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := SocketConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var optionErr error
		if err := raw.Control(func(fd uintptr) { optionErr = syscall.SetsockoptInt(int(fd), SOL_SCTP, SCTP_I_WANT_MAPPED_V4_ADDR, 0) }); err != nil {
			return err
		}
		return optionErr
	}}
	ln, err := cfg.Listen("sctp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	bound, ok := ln.Addr().(*SCTPAddr)
	if !ok {
		t.Fatal("listener has no address")
	}
	assertAddressIPs(t, bound, addr)
	client, err := cfg.Dial("sctp", nil, bound)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.AcceptSCTP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	remote, err := client.SCTPRemoteAddr(0)
	if err != nil {
		t.Fatal(err)
	}
	assertAddressIPs(t, remote, bound)
	local, err := server.SCTPLocalAddr(0)
	if err != nil {
		t.Fatal(err)
	}
	assertAddressIPs(t, local, bound)
	primary, err := client.SCTPGetPrimaryPeerAddr()
	if err != nil || primary == nil || primary.Port != bound.Port {
		t.Fatalf("primary=%v,%v", primary, err)
	}
}

func TestNumericIPv6ZoneEncoding(t *testing.T) {
	for _, zone := range []string{"0", "1", "42", "4294967294"} {
		t.Run(zone, func(t *testing.T) {
			if ifi, err := net.InterfaceByName(zone); err == nil {
				t.Skipf("zone %q names interface %d; name precedence is tested separately", zone, ifi.Index)
			}
			addr, err := ResolveSCTPAddr("sctp6", "[fe80::1%"+zone+"]:9000")
			if err != nil {
				t.Fatal(err)
			}
			raw := addr.ToRawSockAddrBuf()
			want, _ := strconv.ParseUint(zone, 10, 32)
			if got := nativeEndian.Uint32(raw[24:28]); got != uint32(want) {
				t.Fatalf("scope=%d, want %d", got, want)
			}
		})
	}
}

func TestNamedIPv6ZoneRoundTrip(t *testing.T) {
	ifi, err := net.InterfaceByName("lo")
	if err != nil {
		t.Skip(err)
	}
	original := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.ParseIP("fe80::1"), Zone: ifi.Name}}, Port: 9000}
	raw := original.ToRawSockAddrBuf()
	if got := nativeEndian.Uint32(raw[24:28]); got != uint32(ifi.Index) {
		t.Fatalf("scope=%d, want %d", got, ifi.Index)
	}
	decoded, err := resolveFromRawAddr(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.IPAddrs[0].Zone != ifi.Name {
		t.Fatalf("decoded zone=%q", decoded.IPAddrs[0].Zone)
	}
}

func TestEmptyNetwork(t *testing.T) {
	cfg := SocketConfig{Control: func(network, _ string, _ syscall.RawConn) error {
		if network != "sctp" {
			return fmt.Errorf("Control network=%q", network)
		}
		return nil
	}}
	listeners := []struct {
		name   string
		listen func(string, *SCTPAddr) (*SCTPListener, error)
	}{
		{"ListenSCTP", ListenSCTP}, {"ListenSCTPExt", func(n string, a *SCTPAddr) (*SCTPListener, error) { return ListenSCTPExt(n, a, InitMsg{}) }}, {"config", cfg.Listen},
	}
	dialers := []struct {
		name string
		dial func(string, *SCTPAddr, *SCTPAddr) (*SCTPConn, error)
	}{
		{"DialSCTP", DialSCTP}, {"DialSCTPExt", func(n string, l, r *SCTPAddr) (*SCTPConn, error) { return DialSCTPExt(n, l, r, InitMsg{}) }}, {"config", cfg.Dial},
	}
	for _, listener := range listeners {
		t.Run(listener.name, func(t *testing.T) {
			addr, err := ResolveSCTPAddr("", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ln, err := listener.listen("", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			for _, dialer := range dialers {
				t.Run(dialer.name, func(t *testing.T) {
					client, err := dialer.dial("", nil, ln.Addr().(*SCTPAddr))
					if err != nil {
						t.Fatal(err)
					}
					defer client.Close()
					server, err := ln.AcceptSCTP()
					if err != nil {
						t.Fatal(err)
					}
					server.Close()
				})
			}
		})
	}
}

func TestInvalidNetwork(t *testing.T) {
	called := false
	cfg := SocketConfig{Control: func(_, _ string, _ syscall.RawConn) error { called = true; return nil }}
	for _, network := range []string{"tcp", "sctp64", "bad", " "} {
		addr := &SCTPAddr{}
		if _, err := ResolveSCTPAddr(network, "127.0.0.1:0"); err == nil {
			t.Errorf("Resolve accepted %q", network)
		}
		if ln, err := cfg.Listen(network, addr); err == nil {
			ln.Close()
			t.Errorf("Listen accepted %q", network)
		}
		if conn, err := cfg.Dial(network, nil, addr); err == nil {
			conn.Close()
			t.Errorf("Dial accepted %q", network)
		}
	}
	if called {
		t.Fatal("invalid network reached the socket Control hook")
	}
}

func TestPeelOffCloseOnExec(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			ln := lifecycleListener(t)
			fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
			if err != nil {
				t.Fatal(err)
			}
			id, err := SCTPConnect(fd, ln.Addr().(*SCTPAddr))
			if err != nil {
				syscall.Close(fd)
				t.Fatal(err)
			}
			original := NewSCTPConn(fd, nil)
			defer original.Close()
			server, err := ln.AcceptSCTP()
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			var peeled *SCTPConn
			if legacy {
				var peeledFD int
				err = original.withFD(func(fd int) error { var e error; peeledFD, e = peelOffSocketLegacy(fd, id); return e })
				if err == nil {
					peeled = NewSCTPConn(peeledFD, nil)
				}
			} else {
				peeled, err = original.PeelOff(id)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer peeled.Close()
			raw, err := peeled.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			flags, err := fdFlags(lifecycleFD(t, raw))
			if err != nil || flags&syscall.FD_CLOEXEC == 0 {
				t.Fatalf("peeled FD flags=%#x err=%v", flags, err)
			}
			original.Close()
			if _, err := peeled.Write([]byte("peeled")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 32)
			if n, err := server.Read(buf); err != nil || string(buf[:n]) != "peeled" {
				t.Fatalf("peeled data=%q err=%v", buf[:n], err)
			}
		})
	}
}
