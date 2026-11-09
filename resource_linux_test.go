//go:build linux && !386

package sctp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Versioned integration ledger, not a public allocator or a Go heap ceiling.
// See docs/diameterkit-u5-resources.md for slot allowances and caller limits.
func TestU5ResourceLedger(t *testing.T) {
	req, e := MessageIORequirements(MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, Events: messageEventMask, NoDelay: true, AllowFragmentation: true})
	if e != nil {
		t.Fatal(e)
	}
	sizes := map[string]uint64{
		"conn": uint64(unsafe.Sizeof(SCTPConn{})), "listener": uint64(unsafe.Sizeof(SCTPListener{})), "socket_io": uint64(unsafe.Sizeof(socketIO{})), "options": uint64(unsafe.Sizeof(MessageOptions{})), "operation": uint64(unsafe.Sizeof(socketOperation{})), "file_handle": uint64(unsafe.Sizeof(os.File{})), "notification": uint64(unsafe.Sizeof(Notification{})), "config": uint64(unsafe.Sizeof(SocketConfig{})), "address": uint64(unsafe.Sizeof(SCTPAddr{})),
	}
	resident := sizes["conn"] + sizes["socket_io"] + sizes["options"] + sizes["file_handle"]
	work := 2*sizes["operation"] + sizes["notification"] + req.ReadScratchBytes + req.WriteScratchBytes + req.PeekScratchBytes
	// 3 base+2 changed+2 completion channels; two active AfterFunc contexts and
	// two adapter-owned standard operation contexts, one file/poller owner.
	slots := uint64(7*256 + 4*512 + 1024)
	if resident+work+slots > 8192 {
		t.Fatalf("integration reserve drift: resident=%d work=%d slots=%d", resident, work, slots)
	}
	report := struct {
		Go, Arch                                       string
		Sizes                                          map[string]uint64
		Resident, Work, SlotAllowances, LogicalReserve uint64
		IO                                             IORequirements
	}{runtime.Version(), runtime.GOARCH, sizes, resident, work, slots, 8192, req}
	b, e := json.Marshal(report)
	if e != nil {
		t.Fatal(e)
	}
	t.Log(string(b))
}

func TestU5SelectedABI(t *testing.T) {
	if unsafe.Sizeof(SndRcvInfo{}) != 32 || unsafe.Offsetof(SndRcvInfo{}.PPID) != 8 || unsafe.Offsetof(SndRcvInfo{}.AssocID) != 28 {
		t.Fatal("SNDRCV layout drift")
	}
	if unsafe.Sizeof(NotificationHeader{}) != 8 || unsafe.Sizeof(syscall.RawSockaddrInet4{}) != 16 || unsafe.Sizeof(syscall.RawSockaddrInet6{}) != 28 {
		t.Fatal("notification/address layout drift")
	}
	if syscall.CmsgLen(32) != syscall.CmsgLen(0)+32 || syscall.CmsgSpace(32) < syscall.CmsgLen(32) {
		t.Fatal("ancillary layout drift")
	}
	r, e := MessageIORequirements(MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, Events: messageEventMask})
	if e != nil || r.NotificationMin != 148 || r.OOBMin != syscall.CmsgSpace(32) {
		t.Fatal(r, e)
	}
}
func u5Quiescent(t *testing.T, c *SCTPConn) {
	t.Helper()
	c.io.deadlineMu.Lock()
	defer c.io.deadlineMu.Unlock()
	if c.io.readDeadline.owner != nil || c.io.writeDeadline.owner != nil || len(c.io.readGate) != 0 || len(c.io.writeGate) != 0 {
		t.Fatal("operation owner/gate retained after return")
	}
}
func TestU5MessageCancellationRetiresOwners(t *testing.T) {
	c, s := messagePair(t, 0)
	d, b, o := messageBuffers(s)
	// Barrier observes a real read admission before cancellation. Reusing the
	// exact buffers immediately after return also lets race/checkptr detect a
	// surviving borrower. No sleeps or global goroutine-count heuristics.
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		entered := make(chan struct{})
		done := make(chan error, 1)
		var once sync.Once
		go func() {
			_, e := s.readEvent(ctx, d, b, o, func(fd int, p, o []byte, flags int) (int, int, int, error) {
				once.Do(func() { close(entered) })
				return recvMessage(fd, p, o, flags)
			})
			done <- e
		}()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("read never admitted")
		}
		cancel()
		if e := <-done; !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		u5Quiescent(t, s)
		clear(d)
		clear(b)
		clear(o)
		if _, e := c.WriteMsgContext(context.Background(), []byte{byte(i)}, SndInfo{}); e != nil {
			t.Fatal(e)
		}
		r, e := s.ReadEvent(context.Background(), d, b, o)
		if e != nil || r.N != 1 || d[0] != byte(i) {
			t.Fatal(r, e)
		}
		u5Quiescent(t, c)
		u5Quiescent(t, s)
	}
	s.Close()
	u5Quiescent(t, s)
}
