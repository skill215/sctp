package sctp

import (
	"errors"
	"syscall"
	"testing"
)

func TestUnsupportedOwnsWinsock(t *testing.T) {
	var data syscall.WSAData
	if err := syscall.WSAStartup(0x202, &data); err != nil {
		t.Fatal(err)
	}
	defer syscall.WSACleanup()
	socket, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	c := NewSCTPConn(int(socket), nil)
	if _, err := syscall.GetsockoptInt(socket, syscall.SOL_SOCKET, 0x1008 /* SO_TYPE */); err == nil {
		syscall.Closesocket(socket)
		t.Fatal("Winsock handle retained")
	}
	if err := c.Close(); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	replacement, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, syscall.IPPROTO_TCP)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Closesocket(replacement)
	for range 8 {
		if !errors.Is(c.Close(), ErrUnsupported) {
			t.Fatal("missing support error")
		}
	}
	if _, err := syscall.GetsockoptInt(replacement, syscall.SOL_SOCKET, 0x1008 /* SO_TYPE */); err != nil {
		t.Fatal("Close affected live replacement", err)
	}
	failed := NewSCTPConn(1<<30, nil)
	if err := failed.Close(); !errors.Is(err, ErrUnsupported) || err == ErrUnsupported {
		t.Fatalf("native cleanup error lost: %v", err)
	}
}
