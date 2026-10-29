Stream Control Transmission Protocol (SCTP)
----

[![Build Status](https://travis-ci.org/ishidawataru/sctp.svg?branch=master)](https://travis-ci.org/ishidawataru/sctp/builds)

Examples
----

See `example/sctp.go`

```go
$ cd example
$ go build
$ # run example SCTP server
$ ./example -server -port 1000 -ip 10.10.0.1,10.20.0.1
$ # run example SCTP client
$ ./example -port 1000 -ip 10.10.0.1,10.20.0.1
```

Socket ownership and raw I/O
----

`NewSCTPConn` takes ownership of the supplied file descriptor. Close the
connection to release it; do not close the descriptor independently. Internal
sockets use non-blocking descriptors and Go's runtime poller, while `Read`,
`Write`, and `Accept` wait for readiness. Closing a connection or listener wakes
its blocked operations.

`SyscallConn` callbacks follow the `syscall.RawConn` contract. Use non-blocking
system calls inside `Read` and `Write` callbacks, returning `false` on `EAGAIN`
so the runtime can wait for readiness. A callback holds the descriptor alive;
do not close its owner from inside the callback.

`SO_RCVTIMEO` bounds each read and accept; `SO_SNDTIMEO` bounds each write and
connection attempt. Configure them through `SocketConfig.Control` or a
`SyscallConn.Control` callback. The poller uses a separate deadline for each
operation, including time spent queued behind another operation. A zero timeout
means unlimited waiting. I/O timeouts return `EAGAIN`; connection-attempt
timeouts return `EINPROGRESS`, matching the Linux socket API. Timing out does not
leave a deadline on later operations, and closing the owner wakes queued I/O.

Status constants use the Linux SCTP UAPI values. In particular,
`SCTP_ESTABLISHED` is `4` and `SCTP_ACTIVE` is `2`. The old `SCTP_BOUND` and
`SCTP_LISTEN` status constants are deprecated: Linux `SCTP_STATUS` does not
return those states.
