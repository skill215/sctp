//go:build linux && !386

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/skill215/sctp"
)

func TestExampleServerProcess(t *testing.T) {
	encoded := os.Getenv("SCTP_EXAMPLE_SERVER_ARGS")
	if encoded == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		t.Fatal(err)
	}
	flag.CommandLine = flag.NewFlagSet("sctp-example", flag.ExitOnError)
	os.Args = append([]string{"sctp-example"}, args...)
	main()
}

func exampleCommand(t *testing.T, ctx context.Context, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestExampleServerProcess$")
	cmd.Env = append(os.Environ(), "SCTP_EXAMPLE_SERVER_ARGS="+string(encoded))
	return cmd
}

func TestServerRejectsInvalidAddresses(t *testing.T) {
	for _, address := range []string{"%", "127.0.0.1,%"} {
		t.Run(address, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			output, err := exampleCommand(t, ctx, "-server", "-ip", address, "-port", "0").CombinedOutput()
			if err == nil || ctx.Err() != nil || strings.Contains(string(output), "Listen on") || !strings.Contains(string(output), "failed to resolve address") {
				t.Fatalf("invalid address did not fail before listening: error=%v context=%v\n%s", err, ctx.Err(), output)
			}
		})
	}
}

func TestServerKeepsBufferSettingsAcrossClients(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			args := []string{"-server", "-ip", "127.0.0.1", "-port", "0"}
			if explicit {
				args = append(args, "-sndbuf", "4096", "-rcvbuf", "4096")
			}
			cmd := exampleCommand(t, ctx, args...)
			pipe, err := cmd.StderrPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			lines := make(chan string, 256)
			go func() {
				defer close(lines)
				scanner := bufio.NewScanner(pipe)
				for scanner.Scan() {
					select {
					case lines <- scanner.Text():
					case <-ctx.Done():
						return
					}
				}
			}()
			var output []string
			defer func() {
				cancel()
				cmd.Wait()
				for line := range lines {
					output = append(output, line)
				}
				if t.Failed() {
					t.Log(strings.Join(output, "\n"))
				}
			}()
			until := func(prefix string) string {
				t.Helper()
				for {
					select {
					case line, ok := <-lines:
						if !ok {
							t.Fatal("example exited before " + prefix)
						}
						output = append(output, line)
						if _, value, ok := strings.Cut(line, prefix); ok {
							return value
						}
					case <-ctx.Done():
						t.Fatal("timed out waiting for " + prefix)
					}
				}
			}
			addr, err := sctp.ResolveSCTPAddr("sctp4", until("Listen on "))
			if err != nil {
				t.Fatal(err)
			}
			var firstSend, firstReceive int
			for i := range 3 {
				c, err := sctp.DialSCTP("sctp4", nil, addr)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { c.Close() })
				var send, receive int
				line := until("SndBufSize: ")
				_, scanErr := fmt.Sscanf(line, "%d, RcvBufSize: %d", &send, &receive)
				if explicit {
					if err := c.SetWriteBuffer(4096); err != nil {
						c.Close()
						t.Fatal(err)
					}
					if err := c.SetReadBuffer(4096); err != nil {
						c.Close()
						t.Fatal(err)
					}
					wantSend, e1 := c.GetWriteBuffer()
					wantReceive, e2 := c.GetReadBuffer()
					if e1 != nil || e2 != nil || send != wantSend || receive != wantReceive {
						c.Close()
						t.Fatalf("client %d: actual=%d,%d independent control=%d,%d errors=%v,%v", i, send, receive, wantSend, wantReceive, e1, e2)
					}
				}
				c.Close()
				if scanErr != nil {
					t.Fatal(scanErr)
				}
				if i == 0 {
					firstSend, firstReceive = send, receive
				} else if send != firstSend || receive != firstReceive {
					t.Fatalf("client %d changed settings: first=%d,%d got=%d,%d", i, firstSend, firstReceive, send, receive)
				}
			}
		})
	}
}
