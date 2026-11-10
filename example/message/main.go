// Command message demonstrates the v0.8.0 bounded setup and message APIs.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/skill215/sctp"
)

func main() {
	server := flag.Bool("server", false, "listen and echo one association at a time")
	network := flag.String("network", "sctp4", "sctp, sctp4, or sctp6")
	addr := flag.String("addr", "127.0.0.1:9000", "remote endpoint, or local endpoint with -server; use / for multihoming")
	local := flag.String("local", "", "optional local dial endpoint, e.g. 127.0.0.1:0")
	count := flag.Int("count", 1, "number of client messages")
	stream := flag.Uint("stream", 0, "outbound stream ID; checked against negotiated streams")
	ppid := flag.Uint64("ppid", 42, "host-order PPID, including zero")
	payload := flag.String("message", "hello SCTP", "nonempty client payload")
	timeout := flag.Duration("timeout", 5*time.Second, "setup budget and per-message I/O budget (notifications do not refresh it)")
	chunk := flag.Int("chunk", 1024, "receive fragment buffer bytes")
	limit := flag.Int("max-message", 64*1024, "maximum reassembled message bytes (up to 1 MiB)")
	flag.Parse()
	if *count < 1 || *stream > 65535 || *ppid > 0xffffffff || *timeout <= 0 || *limit < 1 || *limit > 1<<20 || *chunk < 1 || *chunk > *limit || len(*payload) == 0 || len(*payload) > *limit {
		log.Fatal("invalid count, stream, PPID, timeout, buffer limit, or payload")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options := sctp.MessageOptions{
		ReceiveInfo: sctp.ReceiveInfoSNDRCV, NoDelay: true, AllowFragmentation: true,
		Events: sctp.SCTP_EVENT_ASSOCIATION | sctp.SCTP_EVENT_ADDRESS |
			sctp.SCTP_EVENT_SEND_FAILURE | sctp.SCTP_EVENT_SHUTDOWN | sctp.SCTP_EVENT_PARTIAL_DELIVERY,
	}
	cfg := sctp.SetupConfig{
		Socket: sctp.SocketConfig{Messages: &options, InitMsg: sctp.InitMsg{NumOstreams: 8, MaxInstreams: 8}},
		Limits: sctp.AddressLimits{MaxAddresses: 16, MaxZoneBytes: 256, MaxAddressBytes: 4096},
	}
	req, err := sctp.SetupRequirements(cfg)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("ledger=%s listener=%d candidate=%d connection=%d (caller buffers extra)",
		req.LedgerVersion, req.ListenerBytes, req.DialerBytes+max(req.EstablishBytes, req.ConnectionBytes), req.ConnectionBytes)
	// A production server reserves these charges before constructing each object.
	// This example bounds concurrency to one connection and allocates its buffers once.
	r := newReader(*chunk, *limit, req.IORequirements)
	if *server {
		err = serve(ctx, *network, *addr, cfg, r, *timeout)
	} else {
		err = exchange(ctx, *network, *local, *addr, cfg, r, *timeout, *count,
			[]byte(*payload), sctp.SndInfo{SID: uint16(*stream), PPID: uint32(*ppid)})
	}
	if err != nil && !(ctx.Err() != nil && errors.Is(err, context.Canceled)) {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, network, addr string, cfg sctp.SetupConfig, r *reader, timeout time.Duration) error {
	setup, cancel := context.WithTimeout(ctx, timeout)
	ln, err := sctp.ListenContext(setup, network, addr, cfg)
	cancel() // successful handoff leaves ln owned by this function
	if err != nil {
		return err
	}
	defer ln.Close()
	bound, err := ln.BoundAddr()
	if err != nil {
		return err
	}
	log.Printf("listening on %s", bound)
	for {
		// Ctrl+C cancels an idle accept without polling. No setup timer is reused.
		conn, err := ln.AcceptSCTPContext(ctx)
		if err != nil {
			return err
		}
		err = echo(ctx, conn, r, timeout)
		conn.Close() // retire before accepting another association
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
			log.Printf("connection ended: %v", err)
		}
	}
}

func echo(ctx context.Context, conn *sctp.SCTPConn, r *reader, timeout time.Duration) error {
	for {
		// One budget covers all fragments/notifications and the echo send.
		operation, cancel := context.WithTimeout(ctx, timeout)
		payload, info, err := r.receive(operation, conn)
		if err == nil {
			// Only the unordered bit is a send flag; do not replay receive-only flags.
			err = send(operation, conn, payload, sctp.SndInfo{
				SID: info.Stream, PPID: info.PPID, Flags: info.Flags & sctp.SCTP_UNORDERED,
			})
		}
		cancel()
		if err != nil {
			return err
		}
	}
}

func exchange(ctx context.Context, network, local, remote string, cfg sctp.SetupConfig, r *reader, timeout time.Duration, count int, payload []byte, info sctp.SndInfo) error {
	dialer, err := sctp.NewDialer(network, local, remote, cfg)
	if err != nil {
		return err
	}
	setup, cancel := context.WithTimeout(ctx, timeout)
	conn, err := dialer.DialContext(setup)
	cancel() // this does not close the handed-off connection
	if err != nil {
		return err
	}
	defer conn.Close()
	status, err := conn.GetStatus()
	if err != nil {
		return err
	}
	if info.SID >= status.Ostreams || info.SID >= status.Instreams {
		return fmt.Errorf("echo stream %d requires both directions; negotiated out=%d in=%d", info.SID, status.Ostreams, status.Instreams)
	}
	for range count {
		operation, done := context.WithTimeout(ctx, timeout)
		err := send(operation, conn, payload, info)
		var reply []byte
		var metadata sctp.SndRcvInfo
		if err == nil {
			reply, metadata, err = r.receive(operation, conn)
		}
		done()
		if err != nil {
			return err
		}
		if !bytes.Equal(reply, payload) || metadata.Stream != info.SID || metadata.PPID != info.PPID {
			return fmt.Errorf("echo payload or metadata mismatch")
		}
		fmt.Printf("payload=%q stream=%d ppid=%d eor=true\n", reply, metadata.Stream, metadata.PPID)
	}
	return nil
}

func send(ctx context.Context, conn *sctp.SCTPConn, payload []byte, info sctp.SndInfo) error {
	n, err := conn.WriteMsgContext(ctx, payload, info)
	if err != nil {
		return fmt.Errorf("send committed %d/%d bytes (no retry): %w", n, len(payload), err)
	}
	if n != len(payload) {
		return io.ErrShortWrite // never send the tail as another SCTP message
	}
	return nil
}
