package sctp_test

import (
	"context"
	"fmt"
	"time"

	"github.com/skill215/sctp"
)

func ExampleResolveSCTPAddrContext() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addr, err := sctp.ResolveSCTPAddrContext(ctx, "sctp4", "127.0.0.1/127.0.0.2:3868")
	if err != nil {
		panic(err)
	}
	fmt.Println(addr.String())
	// Output: 127.0.0.1/127.0.0.2:3868
}

func ExampleSocketConfig_DialContext() {
	// One context covers resolution and connection establishment. This example
	// requires a local SCTP server and is compile-checked without making a dial.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addr, err := sctp.ResolveSCTPAddrContext(ctx, "sctp4", "127.0.0.1:3868")
	if err != nil {
		return
	}
	cfg := sctp.SocketConfig{InitMsg: sctp.InitMsg{NumOstreams: 2}}
	conn, err := cfg.DialContext(ctx, "sctp4", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()
	cancel() // the successfully handed-off connection remains owned by the caller
	// Set I/O deadlines separately; the dial context no longer controls conn.
	_ = conn.SetDeadline(time.Now().Add(time.Second))
}
