//go:build linux && !386

package sctp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func boundedTestConfig() SetupConfig {
	return SetupConfig{Limits: AddressLimits{16, 256, 4096}, Socket: SocketConfig{Messages: &MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, NoDelay: true, AllowFragmentation: true}, InitMsg: InitMsg{NumOstreams: 3, MaxInstreams: 5}}}
}
func TestSetupLimitBoundaries(t *testing.T) {
	cfg := boundedTestConfig()
	for _, limits := range []AddressLimits{{1, 0, 1}, {64, 4096, 16384}, {16, 256, 4096}} {
		cfg.Limits = limits
		r, e := SetupRequirements(cfg)
		if e != nil || r.ConnectionBytes == 0 || r.EstablishBytes < r.ConnectionBytes {
			t.Fatal(r, e)
		}
	}
	for _, limits := range []AddressLimits{{0, 0, 1}, {65, 0, 1}, {1, -1, 1}, {1, 4097, 1}, {1, 0, 0}, {1, 0, 16385}, {int(^uint(0) >> 1), 1, 1}} {
		cfg.Limits = limits
		if _, e := SetupRequirements(cfg); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(limits, e)
		}
	}
	cfg = boundedTestConfig()
	for _, limit := range []int{1, 16, 64} {
		cfg.Limits.MaxAddresses = limit
		cfg.Limits.MaxAddressBytes = 16384
		for _, n := range []int{limit - 1, limit, limit + 1} {
			if n < 1 {
				continue
			}
			remote := strings.Repeat("127.0.0.1/", n-1) + "127.0.0.1:9"
			_, e := NewDialer("sctp4", "", remote, cfg)
			if (e == nil) != (n <= limit) {
				t.Fatal(n, limit, e)
			}
		}
	}
	cfg = boundedTestConfig()
	cfg.Limits.MaxAddresses = 1
	if _, e := NewDialer("sctp4", "127.0.0.1:0", "127.0.0.1:9", cfg); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal("combined hosts", e)
	}
	for _, limit := range []int{0, 1, 256, 4096} {
		cfg = boundedTestConfig()
		cfg.Limits.MaxZoneBytes = limit
		cfg.Limits.MaxAddressBytes = 16384
		for _, n := range []int{limit - 1, limit, limit + 1} {
			if n < 0 {
				continue
			}
			host := "[::1"
			if n > 0 {
				host += "%" + strings.Repeat("z", n)
			}
			host += "]:9"
			_, e := NewDialer("sctp6", "", host, cfg)
			if (e == nil) != (n <= limit) {
				t.Fatal(n, limit, e)
			}
		}
	}
	cfg = boundedTestConfig()
	cfg.Limits.MaxZoneBytes = 3
	if _, e := NewDialer("sctp6", "[::1%aa]:0", "[::1%bb]:9", cfg); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal("combined zones", e)
	}
	for _, limit := range []int{32, 4096, 16384} {
		cfg = boundedTestConfig()
		cfg.Limits.MaxAddressBytes = limit
		for _, n := range []int{limit - 1, limit, limit + 1} {
			remote := strings.Repeat("a", n-2) + ":9"
			_, e := NewDialer("sctp", "", remote, cfg)
			if (e == nil) != (n <= limit) {
				t.Fatal(n, limit, e)
			}
		}
		cfg.Limits.MaxAddressBytes = limit
		remote := strings.Repeat("a", limit-2) + ":9"
		if _, e := NewDialer("sctp", ":0", remote, cfg); e == nil {
			t.Fatal("combined text")
		}
	}
	for _, remote := range []string{"", ":9", "host:0", "host:+1", "host:-1", "host:http", "host:65536", "host:", "/host:9", "host/:9", "host//host:9", "[::1%]:9", "[::1:9", "host:99999999999999999999"} {
		if d, e := NewDialer("sctp", "", remote, boundedTestConfig()); d != nil || !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(remote, d, e)
		}
	}
	for _, network := range []string{"", "tcp", "sctp7"} {
		if _, e := NewDialer(network, "", "127.0.0.1:9", boundedTestConfig()); e == nil {
			t.Fatal(network)
		}
	}
}
func TestSetupOwnershipAndNoIOQuery(t *testing.T) {
	cfg := boundedTestConfig()
	backing := strings.Repeat("x", 1<<20) + "127.0.0.1:9"
	text := backing[len(backing)-11:]
	d, e := NewDialer("sctp4", "", text, cfg)
	if e != nil {
		t.Fatal(text, e)
	}
	if unsafe.StringData(d.remote) == unsafe.StringData(text) {
		t.Fatal("substring retained")
	}
	cfg.Socket.Messages.NoDelay = false
	cfg.Socket.InitMsg.NumOstreams = 1
	if !d.cfg.Socket.Messages.NoDelay || d.cfg.Socket.InitMsg.NumOstreams != 3 {
		t.Fatal("config aliased")
	}
	cfg.Socket.Control = func(string, string, syscall.RawConn) error { t.Fatal("callback ran"); return nil }
	if _, e := NewDialer("sctp", "", "127.0.0.1:9", cfg); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	cfg = boundedTestConfig()
	cfg.Socket.NotificationHandler = func([]byte) error { t.Fatal("callback ran"); return nil }
	if _, e := SetupRequirements(cfg); !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(e)
	}
	for _, network := range []string{"sctp4", "sctp6"} {
		wrong := net.ParseIP("::1")
		if network == "sctp6" {
			wrong = net.ParseIP("127.0.0.1")
		}
		if e := validateSetupResolved(network, boundedTestConfig().Limits, &SCTPAddr{IPAddrs: []net.IPAddr{{IP: wrong}}}); !errors.Is(e, ErrInvalidConfig) {
			t.Fatal(e)
		}
	}
	cfg = boundedTestConfig()
	r, e := SetupRequirements(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("ledger=%+v", r)
	allocs := testing.AllocsPerRun(100, func() { _, _ = SetupRequirements(cfg) })
	t.Logf("pure query allocations=%g (not RSS)", allocs)
	// Old retired connection + parser is in ConnectionBytes, listener and a new
	// candidate coexist; no subtraction/reacquisition window during replacement.
	peak := r.ListenerBytes + r.ConnectionBytes + r.DialerBytes + max(r.EstablishBytes, r.ConnectionBytes)
	if peak <= r.ListenerBytes+r.EstablishBytes {
		t.Fatal("missing overlap")
	}
	t.Logf("replacement upstream peak=%d", peak)
}
func TestSetupBlockedDNSCancellation(t *testing.T) {
	d, e := NewDialer("sctp4", "127.0.0.1:0", "blocked.invalid:9", boundedTestConfig())
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	var once sync.Once
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := d.dialContext(ctx, resolver); done <- e }()
	deadlineEntered(t, entered)
	cancel()
	if e := deadlineResult(t, done); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if c, e := d.DialContext(expired); c != nil || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(c, e)
	}
	if ln, e := ListenContext(expired, "sctp4", "127.0.0.1:0", boundedTestConfig()); ln != nil || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(ln, e)
	}
}
func TestSetupNativeFacade(t *testing.T) {
	for _, network := range []string{"sctp4", "sctp6"} {
		t.Run(network, func(t *testing.T) {
			local := "127.0.0.1:0"
			if network == "sctp6" {
				local = "[::1]:0"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cfg := boundedTestConfig()
			ln, e := ListenContext(ctx, network, local, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			a, e := ln.BoundAddr()
			if e != nil {
				t.Fatal(e)
			}
			d, e := NewDialer(network, "", a.String(), cfg)
			if e != nil {
				t.Fatal(e)
			}
			c, e := d.DialContext(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			s, e := ln.AcceptSCTPContext(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			cancel()
			for _, v := range []*SCTPConn{c, s} {
				u5Quiescent(t, v)
				state, e := v.GetMessageOptions()
				if e != nil || state.Options != *cfg.Socket.Messages {
					t.Fatal(state, e)
				}
				v.withFD(func(fd int) error {
					for _, opt := range []int{syscall.SO_RCVTIMEO, syscall.SO_SNDTIMEO} {
						n, e := socketTimeout(fd, opt)
						if e != nil || n != 0 {
							t.Fatal(n, e)
						}
					}
					return nil
				})
				v.SetDeadline(time.Now().Add(time.Second))
			}
			if _, e := c.WriteMsgContext(context.Background(), []byte("ready"), SndInfo{SID: 1, PPID: 46}); e != nil {
				t.Fatal(e)
			}
			data, b, o := messageBuffers(s)
			r, e := s.ReadEvent(context.Background(), data, b, o)
			if e != nil || string(data[:r.N]) != "ready" || r.Info.Stream != 1 || r.Info.PPID != 46 {
				t.Fatal(r, e)
			}
		})
	}
}

func TestSetupResolutionFamiliesZonesAndOwnership(t *testing.T) {
	cfg := boundedTestConfig()
	for _, tc := range []struct{ network, text string }{{"sctp4", "127.0.0.1/127.0.0.2:9"}, {"sctp6", "[::1%1]/[::1%2]:9"}, {"sctp", "localhost:9"}} {
		d, e := NewDialer(tc.network, "", tc.text, cfg)
		if e != nil {
			t.Fatal(e)
		}
		a, e := resolveSetupEndpoint(context.Background(), d.network, d.remote, &net.Resolver{PreferGo: true})
		if e != nil {
			t.Fatal(e)
		}
		if e = validateSetupResolved(d.network, cfg.Limits, a); e != nil {
			t.Fatal(e)
		}
		want := strings.Count(tc.text, "/") + 1
		if len(a.IPAddrs) != want {
			t.Fatal("DNS expanded explicit host", a)
		}
		if tc.network == "sctp6" && a.IPAddrs[0].Zone != "1" {
			t.Fatal(a)
		}
	}
	cfg.Limits = AddressLimits{64, 4096, 16384}
	r, e := SetupRequirements(cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("maximum ledger=%+v", r)
	cfg = boundedTestConfig()
	bytesPerOp := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			d, e := NewDialer("sctp4", "127.0.0.1:0", "127.0.0.1:9", cfg)
			if e != nil || d == nil {
				b.Fatal(e)
			}
		}
	}).AllocedBytesPerOp()
	t.Logf("small numeric builder allocated bytes/op=%d; allocator measurement, not logical/RSS guarantee", bytesPerOp)
}
