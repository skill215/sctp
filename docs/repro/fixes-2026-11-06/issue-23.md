## Confirmed defect

On 64-bit Linux, the public int association ID parameters are silently narrowed to the signed 32-bit Linux sctp_assoc_t ABI. An out-of-range ID whose low 32 bits match a live association selects that association instead of returning an error.

Affected APIs: PeelOff, SCTPLocalAddr and SCTPRemoteAddr. The destructive case is PeelOff(id + 2^32): it removes the real association from its one-to-many endpoint and returns a working peeled connection. id - 2^32 aliases the same target as well.

Baseline: v0.7.3, commit e4154064dc9928274bcf4615c5382dd6fd81ad21. Reconfirmed on Linux/amd64 with Go 1.26.10 on 2026-11-06. Earlier independent Go 1.26.10 and 1.27.3 probes also reproduced both signs of the offset. This report concerns invalid local arguments; no remote exploit or failure of valid IDs is claimed.

## Reproduction using public APIs

Save the following as main.go in a temporary module requiring github.com/skill215/sctp v0.7.3 and run go run . on 64-bit Linux with kernel SCTP support. All connections are own-process loopback SCTP associations; no external service is required.

```go
package main

import (
	"fmt"
	sctp "github.com/skill215/sctp"
	"net"
	"strconv"
	"syscall"
	"time"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	if strconv.IntSize != 64 {
		panic("requires 64-bit int")
	}
	addr := &sctp.SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}}
	ln, err := sctp.ListenSCTP("sctp4", addr)
	check(err)
	defer ln.Close()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
	check(err)
	owned := true
	defer func() {
		if owned {
			syscall.Close(fd)
		}
	}()
	tv := syscall.NsecToTimeval(int64(time.Second))
	check(syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, &tv))
	check(sctp.SCTPBind(fd, addr, sctp.SCTP_BINDX_ADD_ADDR))
	id, err := sctp.SCTPConnect(fd, ln.Addr().(*sctp.SCTPAddr))
	check(err)
	endpoint := sctp.NewSCTPConn(fd, nil)
	owned = false
	defer endpoint.Close()
	peer, err := ln.AcceptSCTP()
	check(err)
	defer peer.Close()
	raw, err := peer.SyscallConn()
	check(err)
	var optionErr error
	check(raw.Control(func(fd uintptr) {
		optionErr = syscall.SetsockoptTimeval(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	}))
	check(optionErr)
	bad := int(int64(id) + (int64(1) << 32))
	for _, q := range []struct {
		name string
		call func(int) (*sctp.SCTPAddr, error)
	}{{"local", endpoint.SCTPLocalAddr}, {"remote", endpoint.SCTPRemoteAddr}} {
		_, validErr := q.call(id)
		_, invalidErr := q.call(bad)
		fmt.Printf("%s: validID=%d error=%v; invalidID=%d error=%v\n", q.name, id, validErr, bad, invalidErr)
	}
	peeled, err := endpoint.PeelOff(bad)
	fmt.Printf("PeelOff(%d): returnedConnection=%v error=%v\n", bad, peeled != nil, err)
	check(err)
	defer peeled.Close()
	status, err := peeled.GetStatus()
	check(err)
	_, err = peeled.Write([]byte("live association peeled"))
	check(err)
	b := make([]byte, 128)
	n, err := peer.Read(b)
	check(err)
	fmt.Printf("originalID=%d peeledID=%d peerReceived=%q\n", id, status.AssocID, b[:n])
}
```

Actual output from the independent reproduction:

```text
local: validID=15 error=<nil>; invalidID=4294967311 error=<nil>
remote: validID=15 error=<nil>; invalidID=4294967311 error=<nil>
PeelOff(4294967311): returnedConnection=true error=<nil>
originalID=15 peeledID=15 peerReceived="live association peeled"
```

Association IDs and ports vary per run. Expected: reject the out-of-range argument before issuing the target operation; leave the valid association in place and usable.

## Cause and scope

- sctp_linux.go peelOffSocket and peelOffSocketLegacy narrow id with int32(id).
- sctp.go querySCTPAddrs writes uint32(id) directly into the request header.
- The public int is wider than sctp_assoc_t on 64-bit targets.

This is grouped as one association-target validation defect across the affected APIs. It is separate from #21 (INITMSG option values) and #7 (ports); the affected parameters and the unintended operation on a live association differ.

## Fix acceptance

- Reject association IDs that cannot be represented by the existing signed 32-bit ABI, before any request can target an association.
- Cover id + 2^32, id - 2^32, minimum/maximum int values and all affected APIs on 64-bit platforms.
- Preserve valid signed 32-bit/default ID semantics and existing API signatures; do not introduce broad negative-ID policy changes as part of this fix.
- After rejecting bad IDs, verify the original association still exists and can be peeled with its valid ID and exchange payloads.
- Verify unknown in-range IDs retain kernel errors, and run the race suite and ABI/architecture checks.

A local working-tree fix and before/after regressions are being validated. This issue does not claim a published fix or release.
