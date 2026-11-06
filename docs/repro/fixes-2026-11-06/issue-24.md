## Confirmed test defect (not a production transport failure)

The existing one-to-many test setup uses blocking SCTPConnect and treats every non-nil error as an unestablished association. With runtime-style SIGURG interruptions, Linux CONNECTX3 can return EISCONN after establishing the association. The tests consequently fail during setup before exercising PeelOff.

A fresh Go 1.26.10 GOGC=1 race run on Linux/amd64 failed TestPeelOffCloseOnExec/legacy=false at its SCTPConnect call:

```text
transport endpoint is already connected
```

An isolated 100-count retry without explicit signals passed. A separate signal-pressure probe then made the condition reproducible: under SIGURG sent to the calling OS thread, both the public wrapper and a direct CONNECTX3 syscall returned EISCONN in all 1000 iterations of their respective runs. A native SCTP_GET_PEER_ADDR_INFO query found a valid nonzero association ID after each error, and the peer accepted the association.

```text
native=false counts=map[transport endpoint is already connected:1000]
native=true counts=map[transport endpoint is already connected:1000]
```

The direct syscall control establishes that this is kernel blocking-call/restart behavior exposed by a low-level API. The bug being filed is the fixture's assumption, not a claim that EISCONN should always be suppressed by SCTPConnect. Production Dial already handles in-progress/interrupted/already-connected results and waits for readiness.

## Scope

Reconfirmed on 2026-11-06 using v0.7.3 source plus the local #19-#23 fixes. SCTPConnect itself was unchanged by those fixes. The same blocking setup is duplicated in lifecycle, peeloff, status, and send/liveness fixtures. This can make validation fail under GC/signal pressure before the intended assertions run.

## Reproduction probe

Place the following in a Linux test file in the repository (it reuses the existing lifecycleListener helper), then run with GOGC=1 and -race. It logs both public-wrapper and native-syscall outcomes and verifies the actual association using an independent peer-address option lookup.

```go
//go:build linux && !386
package sctp
import("testing";"syscall";"runtime";"time";"unsafe")
func TestProbeConnectRestart(t *testing.T){
 ln:=lifecycleListener(t);addr:=ln.Addr().(*SCTPAddr);raw,_:=ln.SyscallConn();if e:=setSocketTimeout(raw,syscall.SO_RCVTIMEO,time.Second);e!=nil{t.Fatal(e)}
 runtime.LockOSThread();defer runtime.UnlockOSThread();tid:=syscall.Gettid();stop:=make(chan struct{});done:=make(chan struct{})
 go func(){defer close(done);tick:=time.NewTicker(100*time.Microsecond);defer tick.Stop();for{select{case <-stop:return;case <-tick.C:syscall.Tgkill(syscall.Getpid(),tid,syscall.SIGURG)}}}();defer func(){close(stop);<-done}()
 for _,native:=range []bool{false,true}{counts:=map[string]int{}
 for range 1000{
 fd,e:=syscall.Socket(syscall.AF_INET,syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC,132);if e!=nil{t.Fatal(e)}
 var id int
 if native{buf:=addr.ToRawSockAddrBuf();param:=GetAddrsOld{AddrNum:int32(len(buf)),Addrs:uintptr(unsafe.Pointer(&buf[0]))};size:=uint32(unsafe.Sizeof(param));_,_,e=getsockopt(fd,SCTP_SOCKOPT_CONNECTX3,uintptr(unsafe.Pointer(&param)),&size);runtime.KeepAlive(buf);id=int(param.AssocID)}else{id,e=SCTPConnect(fd,addr)}
 key:="success";if e!=nil{key=e.Error()};counts[key]++
 if e!=nil{var info PeerAddrinfo;copy(info.Address[:],addr.ToRawSockAddrBuf());size:=uint32(unsafe.Sizeof(info));_,_,lookupErr:=getsockopt(fd,SCTP_GET_PEER_ADDR_INFO,uintptr(unsafe.Pointer(&info)),&size);t.Logf("native=%v connect=%v returnedID=%d actualID=%d lookup=%v",native,e,id,info.AssocID,lookupErr);if lookupErr!=nil{syscall.Close(fd);continue}}
 peer,e:=ln.AcceptSCTP();syscall.Close(fd);if e!=nil{t.Fatal(e)};peer.Close()
 };t.Logf("native=%v counts=%v",native,counts)}
}
```

## Fix acceptance

- Centralize one-to-many setup instead of duplicating blocking-connect assumptions.
- Use non-blocking connect, accept documented in-progress/interruption results, bound all setup waits, and establish readiness with an actual peer payload and DATA_IO metadata.
- Obtain the real association ID from metadata; verify an immediately successful CONNECTX3 ID agrees with it.
- Restore default event/timeout settings after the setup message is drained, so existing tests retain their intended conditions.
- Keep ownership/cleanup correct on all failing branches; do not blindly convert arbitrary EISCONN from public raw calls into success.
- Add a repeated SIGURG setup-plus-peeloff/payload regression and re-run low-GC, race, checkptr and existing peeloff/status/reset tests.

A local fixture fix and regression test are being validated. No published fix or release is claimed by this issue.
