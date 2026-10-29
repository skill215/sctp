//go:build linux && !386
// +build linux,!386

// Copyright 2019 Wataru Ishida. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sctp

import (
	"errors"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestNotificationHandlerAssignmentOnDialing(t *testing.T) {
	network := "sctp"
	addr := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, Port: 54321}
	testErr := errors.New("test error")
	notificationHandler := func([]byte) error { return testErr }

	listener, err := ListenSCTP(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := dialSCTPExtConfig(network, nil, addr, InitMsg{}, nil, notificationHandler)
	if err != nil {
		t.Fatalf("failed to establish connection due to: %v", err)
	}
	if conn == nil || conn.notificationHandler(nil) != testErr {
		t.Fatalf("notification handler has not been assigned")
	}
	listener.Close()
	conn.Close()
}

func TestNotificationHandlerAssignmentOnListening(t *testing.T) {
	network := "sctp"
	addr := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, Port: 54321}
	testErr := errors.New("test error")
	notificationHandler := func([]byte) error { return testErr }

	listener, err := listenSCTPExtConfig(network, addr, InitMsg{}, nil, notificationHandler)
	if err != nil {
		t.Fatalf("failed to start listening due to: %v", err)
	}
	if listener == nil || listener.notificationHandler(nil) != testErr {
		t.Fatalf("notification handler has not been assigned")
	}
	listener.Close()
}

func TestDialUseControlFuncWithoutLocalAddress(t *testing.T) {
	network := "sctp"
	raddr := &SCTPAddr{IPAddrs: []net.IPAddr{net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}}}
	initMsg := InitMsg{}
	customControlFunc := validationControlFunc(t, network)
	conn, err := dialSCTPExtConfig(network, nil, raddr, initMsg, customControlFunc, nil)
	if err != nil && !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("failed to dial connection due to: %v", err)
	}
	conn.Close()
}

func TestListenUseControlFuncWithoutLocalAddress(t *testing.T) {
	network := "sctp"
	initMsg := InitMsg{}
	customControlFunc := validationControlFunc(t, network)
	listener, err := listenSCTPExtConfig(network, nil, initMsg, customControlFunc, nil)
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Close()
}

func validationControlFunc(t *testing.T, network string) func(networkFunc, address string, c syscall.RawConn) error {
	return func(networkFunc, address string, c syscall.RawConn) error {
		if networkFunc != network {
			t.Errorf("unexpected network: got %s, want %s", networkFunc, network)
		}
		if address != "" {
			t.Error("expected empty address")
		}
		if c == nil {
			t.Error("RawConn is nil")
		}
		return nil
	}
}

func TestSyscallConn(t *testing.T) {
	network := "sctp"
	addr := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, Port: 54321}
	listener, err := ListenSCTP(network, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	raw, err := listener.SyscallConn()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if raw == nil {
		t.Fatalf("Expected non-nil RawConn, got nil")
	}
	conn, err := DialSCTP(network, nil, addr)
	if err != nil {
		t.Fatalf("Failed to create SCTP connection: %v", err)
	}
	defer conn.Close()

	raw, err = conn.SyscallConn()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if raw == nil {
		t.Fatalf("Expected non-nil RawConn, got nil")
	}

	controlCalled := false
	err = raw.Control(func(fd uintptr) {
		controlCalled = true
	})
	if err != nil {
		t.Fatalf("Control failed: %v", err)
	}
	if !controlCalled {
		t.Errorf("Control callback was not called")
	}

	t.Run("after close", func(t *testing.T) {
		conn.Close()
		raw, err := conn.SyscallConn()
		if err != syscall.EINVAL {
			t.Errorf("Expected EINVAL, got %v", err)
		}
		if raw != nil {
			t.Errorf("Expected nil RawConn, got %v", raw)
		}
	})
}

func TestSCTPListenerNameFromFd(t *testing.T) {
	addr := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, Port: 54321}
	ln, err := ListenSCTP("sctp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	la, ok := ln.Addr().(*SCTPAddr)
	if !ok || la.Port == 0 {
		t.Fatalf("got %v; expected a proper address with non-zero port number", la)
	}

	raw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}

	var fln *SCTPListener
	var fileErr error
	err = raw.Control(func(fd uintptr) {
		// NewFile owns its fd, so give it a copy rather than ln's fd.
		dupFD, dupErr := syscall.Dup(int(fd))
		if dupErr != nil {
			fileErr = dupErr
			return
		}
		file := os.NewFile(uintptr(dupFD), "listener")
		defer file.Close()
		fln, fileErr = FileListener(file)
	})
	if err != nil {
		t.Fatal(err)
	}
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	defer fln.Close()

	// Closing the temporary file and collecting it must leave both listeners open.
	runtime.GC()
	if original, ok := ln.Addr().(*SCTPAddr); !ok || original.String() != la.String() {
		t.Fatalf("original listener was closed by the temporary file: %v", original)
	}

	fla, ok := fln.Addr().(*SCTPAddr)
	if !ok || fla.Port == 0 {
		t.Fatalf("got %v; expected a proper address with non-zero port number", la)
	}

	if la.String() != fla.String() {
		t.Fatalf("got %v; expected %v", la.String(), fla.String())
	}
}

func TestWaitForSCTPConnect(t *testing.T) {
	for _, refused := range []bool{false, true} {
		name := "connected"
		if refused {
			name = "refused"
		}
		t.Run(name, func(t *testing.T) {
			addr, err := ResolveSCTPAddr("sctp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ln, err := ListenSCTP("sctp", addr)
			if err != nil {
				t.Fatal(err)
			}
			addr = ln.Addr().(*SCTPAddr)
			if refused {
				ln.Close()
			} else {
				defer ln.Close()
			}
			fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK, syscall.IPPROTO_SCTP)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(fd)
			_, err = SCTPConnect(fd, addr)
			if err != nil && err != syscall.EINPROGRESS {
				if refused && err == syscall.ECONNREFUSED {
					return
				}
				t.Fatal(err)
			}
			err = waitForSCTPConnect(fd)
			if refused {
				if err != syscall.ECONNREFUSED {
					t.Fatalf("expected connection refused, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			server, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			server.Close()
		})
	}
}
