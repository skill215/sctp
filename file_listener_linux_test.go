//go:build linux && !386

package sctp

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestFileListenerConcurrentSourceClose(t *testing.T) {
	ln := lifecycleListener(t)
	raw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	for range 1000 {
		var fd int
		var dupErr error
		if err := raw.Control(func(value uintptr) { fd, dupErr = syscall.Dup(int(value)) }); err != nil {
			t.Fatal(err)
		}
		if dupErr != nil {
			t.Fatal(dupErr)
		}
		source := os.NewFile(uintptr(fd), "sctp-import-source")
		start := make(chan struct{})
		release := make(chan struct{})
		closed := make(chan error, 1)
		go func() {
			<-start
			err := source.Close()
			// Encourage reuse of the just-released descriptor by a non-socket.
			file, openErr := os.Open(os.DevNull)
			<-release
			if file != nil {
				openErr = errors.Join(openErr, file.Close())
			}
			closed <- errors.Join(err, openErr)
		}()
		close(start)
		imported, importErr := FileListener(source)
		close(release)
		closeErr := <-closed
		if closeErr != nil {
			if imported != nil {
				imported.Close()
			}
			t.Fatal(closeErr)
		}
		if importErr != nil {
			// The source may close before duplication. RawConn.Control's
			// closing error need not be os.ErrClosed on every Go version.
			continue
		}
		importRaw, err := imported.SyscallConn()
		if err != nil {
			imported.Close()
			t.Fatal(err)
		}
		var protocol int
		var optionErr error
		err = importRaw.Control(func(value uintptr) {
			protocol, optionErr = syscall.GetsockoptInt(int(value), syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
		})
		imported.Close()
		if err != nil || optionErr != nil || protocol != syscall.IPPROTO_SCTP {
			t.Fatalf("imported recycled/non-SCTP descriptor: protocol=%d errors=%v,%v", protocol, err, optionErr)
		}
	}
}

func TestFileListenerSourceOwnership(t *testing.T) {
	ln := lifecycleListener(t)
	raw, err := ln.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fd int
	var dupErr error
	if err := raw.Control(func(value uintptr) { fd, dupErr = syscall.Dup(int(value)) }); err != nil {
		t.Fatal(err)
	}
	if dupErr != nil {
		t.Fatal(dupErr)
	}
	source := os.NewFile(uintptr(fd), "sctp-import-source")
	defer source.Close()
	imported, err := FileListener(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := imported.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Stat(); err != nil {
		t.Fatalf("closing imported listener closed source: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := FileListener(source); err == nil || got != nil {
		if got != nil {
			got.Close()
		}
		t.Fatalf("closed source imported: %v,%v", got, err)
	}
	if got, err := FileListener(nil); err == nil || got != nil {
		t.Fatalf("nil source imported: %v,%v", got, err)
	}
}
