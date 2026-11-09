package sctp

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// ErrInvalidConfig denotes invalid input to the new context-aware entrypoints.
// Legacy entrypoints keep their original validation/error behavior.
var ErrInvalidConfig = errors.New("invalid SCTP configuration")

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidConfig
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if end, ok := ctx.Deadline(); ok && !time.Now().Before(end) {
		return context.DeadlineExceeded
	}
	return nil
}

// ResolveSCTPAddrContext preserves the legacy slash-separated host/last-port
// grammar, returning one address per explicit host, not a DNS-derived multihoming
// group. It uses the Go resolver and returns owned values. Use the same ctx for
// resolution and DialContext to cover both phases with a single total budget.
func ResolveSCTPAddrContext(ctx context.Context, network, addrs string) (*SCTPAddr, error) {
	return resolveSCTPAddrContext(ctx, network, addrs, &net.Resolver{PreferGo: true})
}
func resolveSCTPAddrContext(ctx context.Context, network, addrs string, resolver *net.Resolver) (*SCTPAddr, error) {
	if e := contextError(ctx); e != nil {
		return nil, e
	}
	network, e := normalizeNetwork(network)
	if e != nil {
		return nil, e
	}
	if addrs == "" {
		return &SCTPAddr{}, nil
	}
	parts := strings.Split(addrs, "/")
	result := &SCTPAddr{}
	for j, part := range parts {
		if e = contextError(ctx); e != nil {
			return nil, e
		}
		address := part
		if j < len(parts)-1 {
			address += ":"
		}
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		if len(parts) > 1 && host == "" {
			return nil, &net.AddrError{Err: "empty multihoming host", Addr: addrs}
		}
		if j == len(parts)-1 {
			result.Port, e = resolver.LookupPort(ctx, "tcp", port)
			if e != nil {
				if ce := contextError(ctx); ce != nil {
					return nil, ce
				}
				return nil, e
			}
		}
		if host != "" {
			values, e := resolver.LookupIPAddr(ctx, host)
			if e != nil {
				if ce := contextError(ctx); ce != nil {
					return nil, ce
				}
				return nil, e
			}
			if e = contextError(ctx); e != nil {
				return nil, e
			}
			selected := -1
			for i, v := range values {
				ipv4 := v.IP.To4() != nil
				if network == "sctp4" && !ipv4 || network == "sctp6" && ipv4 {
					continue
				}
				if selected < 0 {
					selected = i
				}
				if network == "sctp" && ipv4 {
					selected = i
					break
				}
			}
			if selected < 0 {
				return nil, &net.AddrError{Err: "no suitable address", Addr: host}
			}
			v := values[selected]
			result.IPAddrs = append(result.IPAddrs, net.IPAddr{IP: append(net.IP(nil), v.IP.To16()...), Zone: v.Zone})
		}
	}
	if e = contextError(ctx); e != nil {
		return nil, e
	}
	return result, nil
}

// DialContext connects pre-resolved owned address copies within ctx. It rejects
// Control before creating a socket: synchronous user callbacks cannot provide a
// cancellable setup contract. The config and addresses must not be concurrently
// modified during the call. Cancellation after successful handoff does not close
// the returned connection. The existing NotificationHandler remains synchronous
// legacy receive behavior; this method does not enable message mode.
func (cfg *SocketConfig) DialContext(ctx context.Context, network string, laddr, raddr *SCTPAddr) (*SCTPConn, error) {
	if e := contextError(ctx); e != nil {
		return nil, e
	}
	if cfg == nil || cfg.Control != nil {
		return nil, ErrInvalidConfig
	}
	owned := *cfg
	messages, configErr := copyMessageConfig(cfg)
	if configErr != nil {
		return nil, configErr
	}
	owned.Messages = messages
	network, e := normalizeNetwork(network)
	if e != nil {
		return nil, e
	}
	if e = validateSCTPAddr(laddr, false); e != nil {
		return nil, e
	}
	if e = validateSCTPAddr(raddr, true); e != nil {
		return nil, e
	}
	local, remote := cloneSCTPAddr(laddr), cloneSCTPAddr(raddr)
	if e = contextError(ctx); e != nil {
		return nil, e
	}
	return dialSCTPContext(ctx, network, local, remote, owned)
}
