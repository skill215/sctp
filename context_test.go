package sctp

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestContextResolveLegacyGrammar(t *testing.T) {
	for _, tc := range resolveSCTPAddrTests {
		got, e := ResolveSCTPAddrContext(context.Background(), tc.network, tc.litAddrOrName)
		if e != nil || !reflect.DeepEqual(got, tc.addr) {
			t.Fatalf("%s: %#v %v", tc.litAddrOrName, got, e)
		}
	}
	for _, s := range []string{"", ":", "localhost:http", "[::1]/[::1]:80"} {
		old, e1 := ResolveSCTPAddr("sctp", s)
		got, e2 := ResolveSCTPAddrContext(context.Background(), "sctp", s)
		if (e1 == nil) != (e2 == nil) || e1 == nil && !reflect.DeepEqual(old, got) {
			t.Fatalf("%s old%+v/%v new%+v/%v", s, old, e1, got, e2)
		}
	}
	for _, s := range []string{"/127.0.0.1:1", "127.0.0.1/:1", "127.0.0.1:-1", "127.0.0.1:65536", "[::1", "host"} {
		if a, e := ResolveSCTPAddrContext(context.Background(), "sctp", s); a != nil || e == nil {
			t.Fatal(s, a, e)
		}
	}
	for _, p := range [][2]string{{"sctp4", "[::1]:0"}, {"sctp6", "127.0.0.1:0"}, {"bad", "127.0.0.1:0"}} {
		if a, e := ResolveSCTPAddrContext(context.Background(), p[0], p[1]); a != nil || e == nil {
			t.Fatal(p, a, e)
		}
	}
}
func TestContextResolveCancellationAndOwnership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a, e := ResolveSCTPAddrContext(ctx, "sctp", "127.0.0.1:0"); a != nil || !errors.Is(e, context.Canceled) {
		t.Fatal(a, e)
	}
	if a, e := ResolveSCTPAddrContext(nil, "sctp", ":0"); a != nil || !errors.Is(e, ErrInvalidConfig) {
		t.Fatal(a, e)
	}
	first, e := ResolveSCTPAddrContext(context.Background(), "sctp", "127.0.0.1/127.0.0.1:9")
	if e != nil {
		t.Fatal(e)
	}
	first.IPAddrs[0].IP[15] = 2
	if !first.IPAddrs[1].IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatal("host slices alias")
	}
	next, e := ResolveSCTPAddrContext(context.Background(), "sctp", "127.0.0.1:9")
	if e != nil || !next.IPAddrs[0].IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatal(next, e)
	}
}
func TestContextResolveBlockedDNSAndSingleBudget(t *testing.T) {
	entered := make(chan struct{})
	var once sync.Once
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := resolveSCTPAddrContext(ctx, "sctp", "127.0.0.1/blocked.example.invalid:9", resolver)
		done <- e
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("resolver not entered")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("DNS cancellation blocked")
	}
	// An exhausted shared budget is not restarted for a later phase/host.
	expired, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel2()
	if _, e := resolveSCTPAddrContext(expired, "sctp", "localhost:9", resolver); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	cfg := SocketConfig{}
	if c, e := cfg.DialContext(expired, "sctp", nil, &SCTPAddr{Port: 9}); c != nil || !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(c, e)
	}
}
