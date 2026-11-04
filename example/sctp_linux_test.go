//go:build linux && !386

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/skill215/sctp"
)

// Run the actual CLI in a subprocess so the regression covers its stream
// selection, PPID counter and fatal-error path together.
func TestClientHonorsNegotiatedStreams(t *testing.T) {
	const childPort = "SCTP_EXAMPLE_TEST_PORT"
	if port := os.Getenv(childPort); port != "" {
		flag.CommandLine = flag.NewFlagSet("sctp-example", flag.ExitOnError)
		os.Args = []string{"sctp-example", "-ip", "127.0.0.1", "-port", port}
		main()
		return
	}
	for _, streams := range []uint16{1, 2} {
		t.Run(fmt.Sprint(streams), func(t *testing.T) {
			addr, err := sctp.ResolveSCTPAddr("sctp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ln, err := sctp.ListenSCTPExt("sctp4", addr, sctp.InitMsg{NumOstreams: streams, MaxInstreams: streams})
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			raw, err := ln.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			setReceiveTimeout(t, raw)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestClientHonorsNegotiatedStreams$")
			cmd.Env = append(os.Environ(), childPort+"="+strconv.Itoa(ln.Addr().(*sctp.SCTPAddr).Port))
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				err := cmd.Wait()
				if t.Failed() {
					t.Logf("CLI exit: %v\n%s", err, output.String())
				}
			}()
			peer, err := ln.AcceptSCTP()
			if err != nil {
				t.Fatal(err)
			}
			defer peer.Close()
			raw, err = peer.SyscallConn()
			if err != nil {
				t.Fatal(err)
			}
			setReceiveTimeout(t, raw)
			if err := peer.SubscribeEvents(sctp.SCTP_EVENT_DATA_IO); err != nil {
				t.Fatal(err)
			}
			status, err := peer.GetStatus()
			if err != nil || status.Instreams != streams {
				t.Fatalf("negotiated status=%+v, error=%v", status, err)
			}
			buf := make([]byte, 512)
			for ppid := uint32(0); ppid < 4; ppid++ {
				n, info, err := peer.SCTPRead(buf)
				if err != nil || n != 256 || info == nil {
					t.Fatalf("message %d: n=%d info=%+v error=%v", ppid, n, info, err)
				}
				if info.PPID != ppid || info.Stream != uint16(ppid%uint32(streams)) {
					t.Fatalf("message %d: stream=%d PPID=%d", ppid, info.Stream, info.PPID)
				}
				if written, err := peer.SCTPWrite(buf[:n], info); err != nil || written != n {
					t.Fatalf("echo %d: n=%d error=%v", ppid, written, err)
				}
			}
		})
	}
}

func setReceiveTimeout(t *testing.T, raw syscall.RawConn) {
	t.Helper()
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		tv := syscall.NsecToTimeval((3 * time.Second).Nanoseconds())
		optionErr = syscall.SetsockoptTimeval(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	}); err != nil {
		t.Fatal(err)
	}
	if optionErr != nil {
		t.Fatal(optionErr)
	}
}
