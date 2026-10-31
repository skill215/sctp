//go:build linux && !386

package sctp

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

func stabilityReadPromptly(t *testing.T, c *SCTPConn, r io.Reader) (int, error) {
	t.Helper()
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { n, err := r.Read(make([]byte, 128)); done <- result{n, err} }()
	select {
	case got := <-done:
		return got.n, got.err
	case <-time.After(300 * time.Millisecond):
		c.Close()
		<-done
		t.Fatal("terminated association kept waiting")
		return 0, nil
	}
}

type stabilitySCTPReader struct{ c *SCTPConn }

func (r stabilitySCTPReader) Read(b []byte) (int, error) { n, _, err := r.c.SCTPRead(b); return n, err }

func TestStabilityReadAfterConsumedReset(t *testing.T) {
	for _, mode := range []string{"Read", "SCTPRead", "wrapped"} {
		for _, consume := range []string{"Write", "raw"} {
			t.Run(mode+"/"+consume, func(t *testing.T) {
				c, p := lifecyclePair(t)
				var r io.Reader = c
				if mode == "SCTPRead" {
					r = stabilitySCTPReader{c}
				} else if mode == "wrapped" {
					r = NewSCTPSndRcvInfoWrappedConn(c)
				}
				stabilityAbort(t, p)
				var resetErr error
				if consume == "Write" {
					_, resetErr = c.Write([]byte("reset"))
				} else {
					if err := c.withFD(func(fd int) error { _, resetErr = syscall.SendmsgN(fd, []byte("reset"), nil, nil, 0); return nil }); err != nil {
						t.Fatal(err)
					}
				}
				if !errors.Is(resetErr, syscall.ECONNRESET) {
					t.Fatalf("trigger=%v", resetErr)
				}
				for range 2 {
					n, err := stabilityReadPromptly(t, c, r)
					if n != 0 || !errors.Is(err, syscall.ENOTCONN) {
						t.Fatalf("terminated read=%d,%v", n, err)
					}
				}
			})
		}
	}
}

func TestStabilityIdleAndQueuedRead(t *testing.T) {
	for _, terminated := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "queued-reset"}[terminated], func(t *testing.T) {
			c, p := lifecyclePair(t)
			if terminated {
				c.io.readGate <- struct{}{}
			}
			done := make(chan error, 1)
			marker := []byte("still alive")
			go func() {
				b := make([]byte, 64)
				n, err := c.Read(b)
				if !terminated && err == nil && !bytes.Equal(b[:n], marker) {
					err = syscall.EIO
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("idle read returned early: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			if terminated {
				stabilityAbort(t, p)
				if _, err := c.Write([]byte("consume")); !errors.Is(err, syscall.ECONNRESET) {
					<-c.io.readGate
					t.Fatal(err)
				}
				<-c.io.readGate
			} else {
				if _, err := p.Write(marker); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if terminated && !errors.Is(err, syscall.ENOTCONN) || !terminated && err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				c.Close()
				<-done
				t.Fatal("read did not finish")
			}
		})
	}
}

func TestStabilityWaitingReadAndConcurrentReset(t *testing.T) {
	c, p := lifecyclePair(t)
	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 64)); done <- err }()
	time.Sleep(10 * time.Millisecond)
	stabilityAbort(t, p)
	_, writeErr := c.Write([]byte("consume if pending"))
	select {
	case err := <-done:
		if !errors.Is(err, syscall.ECONNRESET) && !errors.Is(err, syscall.ENOTCONN) {
			t.Fatalf("read=%v write=%v", err, writeErr)
		}
	case <-time.After(time.Second):
		c.Close()
		<-done
		t.Fatal("active read remained blocked")
	}
}

func TestStabilityOneToManyIdle(t *testing.T) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 132)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	if err := syscall.Listen(fd, 5); err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	endpoint := NewSCTPConn(fd, nil)
	defer endpoint.Close()
	raw, _ := endpoint.SyscallConn()
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if n, err := endpoint.Read(make([]byte, 64)); n != 0 || !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("unassociated endpoint=%d,%v", n, err)
	}
}

func TestStabilityPeeledConsumedReset(t *testing.T) {
	endpoint, p, id := stabilityEndpoint(t)
	peeled, err := endpoint.PeelOff(id)
	if err != nil {
		t.Fatal(err)
	}
	defer peeled.Close()
	stabilityAbort(t, p)
	if _, err := peeled.Write([]byte("consume")); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal(err)
	}
	if n, err := stabilityReadPromptly(t, peeled, peeled); n != 0 || !errors.Is(err, syscall.ENOTCONN) {
		t.Fatalf("peeled=%d,%v", n, err)
	}
}

func TestStabilityImportedConnection(t *testing.T) {
	c, p := lifecyclePair(t)
	var fd int
	if err := c.withFD(func(original int) error { var err error; fd, err = syscall.Dup(original); return err }); err != nil {
		t.Fatal(err)
	}
	imported := NewSCTPConn(fd, nil)
	defer imported.Close()
	if !imported.singleAssociation {
		t.Fatal("imported stream scope unknown")
	}
	stabilityAbort(t, p)
	if _, err := imported.Write([]byte("consume")); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal(err)
	}
	if n, err := stabilityReadPromptly(t, imported, imported); n != 0 || !errors.Is(err, syscall.ENOTCONN) {
		t.Fatalf("imported read=%d,%v", n, err)
	}
}

func TestStabilityBufferedShutdown(t *testing.T) {
	c, p := lifecyclePair(t)
	marker := []byte("queued before shutdown")
	if _, err := p.Write(marker); err != nil {
		t.Fatal(err)
	}
	p.Close()
	b := make([]byte, 128)
	n, err := c.Read(b)
	if err != nil || !bytes.Equal(b[:n], marker) {
		t.Fatalf("buffered payload=%q,%v", b[:max(0, n)], err)
	}
	if n, err := stabilityReadPromptly(t, c, c); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("drained EOF=%d,%v", n, err)
	}
}

func TestStabilityOneToManyAssociation(t *testing.T) {
	c, p, _ := stabilityEndpoint(t)
	if c.singleAssociation {
		t.Fatal("endpoint classified as exclusive")
	}
	if _, err := p.Write([]byte("endpoint data")); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	if n, err := c.Read(b); err != nil || string(b[:n]) != "endpoint data" {
		t.Fatalf("endpoint data=%d,%v", n, err)
	}
	raw, _ := c.SyscallConn()
	if err := setSocketTimeout(raw, syscall.SO_RCVTIMEO, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Read(b); n != 0 || !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("live endpoint=%d,%v", n, err)
	}
}

func TestStabilityShutdownRacesStatusQuery(t *testing.T) {
	for range 50 {
		c, p := lifecyclePair(t)
		done := make(chan error, 1)
		go func() {
			b, err := io.ReadAll(c)
			if err == nil && string(b) != "last data" {
				err = syscall.EIO
			}
			done <- err
		}()
		if _, err := p.Write([]byte("last data")); err != nil {
			t.Fatal(err)
		}
		p.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			c.Close()
			<-done
			t.Fatal("normal shutdown stalled")
		}
		c.Close()
	}
}
