//go:build linux && !386

package sctp

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

func TestStabilityAddressInputs(t *testing.T) {
	for _, network := range []string{"sctp4", "sctp6"} {
		for _, dial := range []bool{false, true} {
			for _, fail := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/dial=%v/fail=%v", network, dial, fail), func(t *testing.T) {
					alias := []net.IPAddr{{IP: net.IP{1, 2, 3, 4}, Zone: "sentinel"}}
					addr := &SCTPAddr{IPAddrs: alias[:0]}
					before := net.IPAddr{IP: append(net.IP(nil), alias[0].IP...), Zone: alias[0].Zone}
					var target *SCTPAddr
					if dial {
						ip := "127.0.0.1"
						if network == "sctp6" {
							ip = "[::1]"
						}
						a, err := ResolveSCTPAddr(network, ip+":0")
						if err != nil {
							t.Fatal(err)
						}
						ln, err := ListenSCTP(network, a)
						if err != nil {
							t.Fatal(err)
						}
						defer ln.Close()
						target = ln.Addr().(*SCTPAddr)
					}
					sentinel := errors.New("control failure")
					cfg := SocketConfig{Control: func(_, observed string, _ syscall.RawConn) error {
						if observed != ":0" {
							t.Errorf("Control saw %q", observed)
						}
						if fail {
							return sentinel
						}
						return nil
					}}
					var wg sync.WaitGroup
					for range 4 {
						wg.Go(func() {
							if dial {
								c, err := cfg.Dial(network, addr, target)
								if c != nil {
									c.Close()
								}
								if (err != nil) != fail {
									t.Errorf("Dial=%v", err)
								}
							} else {
								ln, err := cfg.Listen(network, addr)
								if ln != nil {
									ln.Close()
								}
								if (err != nil) != fail {
									t.Errorf("Listen=%v", err)
								}
							}
						})
					}
					wg.Wait()
					if len(addr.IPAddrs) != 0 || !reflect.DeepEqual(alias[0], before) {
						t.Fatalf("input mutated: %+v alias=%+v", addr, alias)
					}
				})
			}
		}
	}
}

func TestStabilityInvalidEndpointInputs(t *testing.T) {
	valid := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, Port: 9}
	for _, port := range []int{-1, 65536, int(^uint(0) >> 1)} {
		t.Run(fmt.Sprint(port), func(t *testing.T) {
			bad := &SCTPAddr{Port: port}
			calls := 0
			cfg := SocketConfig{Control: func(string, string, syscall.RawConn) error { calls++; return errors.New("Control must not run") }}
			for _, run := range []func() error{
				func() error {
					ln, e := cfg.Listen("sctp4", bad)
					if ln != nil {
						ln.Close()
					}
					return e
				},
				func() error {
					c, e := cfg.Dial("sctp4", bad, valid)
					if c != nil {
						c.Close()
					}
					return e
				},
				func() error {
					c, e := cfg.Dial("sctp4", nil, bad)
					if c != nil {
						c.Close()
					}
					return e
				},
				func() error { return SCTPBind(-1, bad, SCTP_BINDX_ADD_ADDR) },
				func() error { _, e := SCTPConnect(-1, bad); return e },
			} {
				err := run()
				var addrErr *net.AddrError
				if !errors.As(err, &addrErr) {
					t.Errorf("port error=%T %v", err, err)
				}
			}
			if calls != 0 {
				t.Fatalf("invalid input invoked Control %d times", calls)
			}
		})
	}
	for _, run := range []func() (*SCTPConn, error){
		func() (*SCTPConn, error) { return DialSCTP("sctp4", nil, nil) },
		func() (*SCTPConn, error) { return DialSCTPExt("sctp4", nil, nil, InitMsg{}) },
		func() (*SCTPConn, error) {
			return (&SocketConfig{Control: func(string, string, syscall.RawConn) error { t.Error("nil remote invoked Control"); return nil }}).Dial("sctp4", nil, nil)
		},
	} {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("nil remote panic: %v", p)
				}
			}()
			c, err := run()
			if c != nil {
				c.Close()
			}
			var ae *net.AddrError
			if !errors.As(err, &ae) {
				t.Errorf("nil remote error=%v", err)
			}
		}()
	}
}

func TestStabilityMultihomingEmptyHosts(t *testing.T) {
	for _, input := range []string{"127.0.0.1/:0", "/127.0.0.1:0", "127.0.0.1//127.0.0.2:0", "[::1]/:0", "/[::1]:0", "/:0"} {
		if a, err := ResolveSCTPAddr("sctp", input); err == nil {
			t.Errorf("accepted %q as %v", input, a)
		}
	}
	for _, input := range []string{":0", "127.0.0.1:0", "127.0.0.1/127.0.0.2:0", "[::1]:0", "[::1]/[::]:0"} {
		if _, err := ResolveSCTPAddr("sctp", input); err != nil {
			t.Errorf("valid %q: %v", input, err)
		}
	}
}

func TestStabilityLargeAddressQuery(t *testing.T) {
	for _, count := range []int{2, 300} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			addr := &SCTPAddr{}
			for i := range count {
				addr.IPAddrs = append(addr.IPAddrs, net.IPAddr{IP: net.IPv4(127, 0, byte(i/254), byte(i%254+1))})
			}
			ln, err := ListenSCTP("sctp4", addr)
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			// Independent raw large-buffer control confirms the actual complete binding.
			var param struct {
				ID    int32
				Count uint32
				Buf   [16384]byte
			}
			size := uint32(unsafe.Sizeof(param))
			raw, _ := ln.SyscallConn()
			var queryErr error
			if err := raw.Control(func(fd uintptr) {
				_, _, queryErr = getsockopt(int(fd), SCTP_GET_LOCAL_ADDRS, uintptr(unsafe.Pointer(&param)), &size)
			}); err != nil {
				t.Fatal(err)
			}
			if queryErr != nil || int(param.Count) != count {
				t.Fatalf("raw query count=%d err=%v", param.Count, queryErr)
			}
			got := ln.Addr()
			if got == nil || len(got.(*SCTPAddr).IPAddrs) != count {
				t.Fatalf("incomplete Addr: %v", got)
			}
		})
	}
}

func TestStabilityAddressSnapshot(t *testing.T) {
	original := &SCTPAddr{IPAddrs: []net.IPAddr{{IP: net.IP{127, 0, 0, 1}}}}
	cfg := SocketConfig{Control: func(_, observed string, _ syscall.RawConn) error {
		if observed != "127.0.0.1:0" {
			t.Fatalf("address string=%q", observed)
		}
		original.IPAddrs[0].IP[0] = 203
		return nil
	}}
	ln, err := cfg.Listen("sctp4", original)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if got := ln.Addr().(*SCTPAddr); !got.IPAddrs[0].IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("internal snapshot changed: %v", got)
	}
}
