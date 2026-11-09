package sctp

import (
	"context"
	"net"
	"strings"
)

// AddressLimits bounds combined local+remote explicit hosts, zone bytes and text
// bytes before copying or resolving. All limits are explicit: N=1..64,
// Z=0..4096, D=1..16384. DNS internals are outside these logical bounds.
type AddressLimits struct{ MaxAddresses, MaxZoneBytes, MaxAddressBytes int }

// SetupConfig is the bounded setup facade. Control and NotificationHandler are
// rejected; Messages=nil keeps legacy I/O. Inputs must not be concurrently
// modified during construction; constructors freeze all retained values.
type SetupConfig struct {
	Socket SocketConfig
	Limits AddressLimits
}

// Dialer owns frozen endpoint text and configuration. Each call resolves one IP
// per explicit host and makes one association attempt with the original context.
// It does not discover alternative services, retry, reconnect or replay data.
type Dialer struct {
	network, local, remote string
	cfg                    SetupConfig
}

func validateSetupConfig(cfg SetupConfig) error {
	l := cfg.Limits
	if l.MaxAddresses < 1 || l.MaxAddresses > 64 || l.MaxZoneBytes < 0 || l.MaxZoneBytes > 4096 || l.MaxAddressBytes < 1 || l.MaxAddressBytes > 16384 || cfg.Socket.Control != nil || cfg.Socket.NotificationHandler != nil {
		return ErrInvalidConfig
	}
	if cfg.Socket.Messages != nil {
		return validateMessageOptions(*cfg.Socket.Messages)
	}
	return nil
}
func freezeSetupConfig(cfg SetupConfig) SetupConfig {
	if cfg.Socket.Messages != nil {
		m := *cfg.Socket.Messages
		cfg.Socket.Messages = &m
	}
	return cfg
}

// setupHost returns borrowed host/port slices on success. Intermediate slash
// hosts have no port; only the last host has a numeric port. IPv6 requires
// brackets, matching the old grammar.
func setupHost(part string, last bool) (host, port string, err error) {
	if last {
		return net.SplitHostPort(part)
	}
	if len(part) >= 2 && part[0] == '[' && part[len(part)-1] == ']' {
		return part[1 : len(part)-1], "", nil
	}
	if strings.ContainsAny(part, ":[]") {
		return "", "", ErrInvalidConfig
	}
	return part, "", nil
}
func validateSetupEndpoints(network, local, remote string, l AddressLimits, dial bool) error {
	if network != "sctp" && network != "sctp4" && network != "sctp6" || len(local) > l.MaxAddressBytes || len(remote) > l.MaxAddressBytes-len(local) || dial && remote == "" {
		return ErrInvalidConfig
	}
	// Count before splitting/cloning/parsing. Cut below returns borrowed slices.
	n := 0
	for _, s := range [2]string{local, remote} {
		if s != "" {
			n += strings.Count(s, "/") + 1
		}
	}
	if n > l.MaxAddresses {
		return ErrInvalidConfig
	}
	// Count raw zone bytes before any host/port parsing. Valid scoped literals
	// delimit the zone with ']'; slash separates explicit hosts. Malformed input
	// is rejected by the grammar pass below, without retaining the input.
	rawZones := 0
	for _, s := range [2]string{local, remote} {
		for i := 0; i < len(s); i++ {
			if s[i] != '%' {
				continue
			}
			end := i + 1
			for end < len(s) && s[end] != ']' && s[end] != '/' {
				end++
			}
			rawZones += end - i - 1
			if rawZones > l.MaxZoneBytes {
				return ErrInvalidConfig
			}
			i = end - 1
		}
	}
	zones := 0
	for which, s := range [2]string{local, remote} {
		if s == "" {
			continue
		}
		multi := strings.Contains(s, "/")
		for rest := s; ; {
			part, tail, more := strings.Cut(rest, "/")
			host, port, e := setupHost(part, !more)
			if e != nil || host == "" && (multi || dial && which == 1) {
				return ErrInvalidConfig
			}
			if at := strings.LastIndexByte(host, '%'); at >= 0 {
				zone := host[at+1:]
				if zone == "" {
					return ErrInvalidConfig
				}
				zones += len(zone)
				if zones > l.MaxZoneBytes {
					return ErrInvalidConfig
				}
			}
			if !more {
				if port == "" {
					return ErrInvalidConfig
				}
				value := 0
				for i := 0; i < len(port); i++ {
					if port[i] < '0' || port[i] > '9' {
						return ErrInvalidConfig
					}
					value = value*10 + int(port[i]-'0')
					if value > 65535 {
						return ErrInvalidConfig
					}
				}
				if dial && which == 1 && value == 0 {
					return ErrInvalidConfig
				}
				break
			}
			rest = tail
		}
	}
	return nil
}
func validateSetupResolved(network string, l AddressLimits, addrs ...*SCTPAddr) error {
	n, z := 0, 0
	for _, a := range addrs {
		if a == nil {
			continue
		}
		n += len(a.IPAddrs)
		if n > l.MaxAddresses {
			return ErrInvalidConfig
		}
		for _, v := range a.IPAddrs {
			v4 := v.IP.To4() != nil
			if v.IP.To16() == nil || network == "sctp4" && !v4 || network == "sctp6" && v4 || v4 && v.Zone != "" {
				return ErrInvalidConfig
			}
			z += len(v.Zone)
			if z > l.MaxZoneBytes {
				return ErrInvalidConfig
			}
		}
	}
	return nil
}

// NewDialer validates all combined limits before freezing input. It performs no
// DNS or socket I/O. Query SetupRequirements before reserving constructor space.
func NewDialer(network, local, remote string, cfg SetupConfig) (*Dialer, error) {
	if _, e := SetupRequirements(cfg); e != nil {
		return nil, e
	}
	if e := validateSetupEndpoints(network, local, remote, cfg.Limits, true); e != nil {
		return nil, e
	}
	return &Dialer{network: strings.Clone(network), local: strings.Clone(local), remote: strings.Clone(remote), cfg: freezeSetupConfig(cfg)}, nil
}
func (d *Dialer) DialContext(ctx context.Context) (*SCTPConn, error) {
	return d.dialContext(ctx, &net.Resolver{PreferGo: true})
}
func (d *Dialer) dialContext(ctx context.Context, resolver *net.Resolver) (*SCTPConn, error) {
	if e := contextError(ctx); e != nil {
		return nil, e
	}
	if d == nil {
		return nil, ErrInvalidConfig
	}
	local, e := resolveSetupEndpoint(ctx, d.network, d.local, resolver)
	if e != nil {
		return nil, e
	}
	remote, e := resolveSetupEndpoint(ctx, d.network, d.remote, resolver)
	if e != nil {
		return nil, e
	}
	if e = validateSetupResolved(d.network, d.cfg.Limits, local, remote); e != nil {
		return nil, e
	}
	return d.cfg.Socket.DialContext(ctx, d.network, local, remote)
}
func resolveSetupEndpoint(ctx context.Context, network, text string, resolver *net.Resolver) (*SCTPAddr, error) {
	if text == "" {
		return nil, contextError(ctx)
	}
	return resolveSCTPAddrContext(ctx, network, text, resolver)
}

// ListenContext validates and freezes bounded input, resolves, binds, verifies
// options and publishes within one original context. Cancelling after handoff
// does not close the listener. Concurrent constructors require caller admission.
func ListenContext(ctx context.Context, network, local string, cfg SetupConfig) (*SCTPListener, error) {
	if e := contextError(ctx); e != nil {
		return nil, e
	}
	if _, e := SetupRequirements(cfg); e != nil {
		return nil, e
	}
	if e := validateSetupEndpoints(network, local, "", cfg.Limits, false); e != nil {
		return nil, e
	}
	network, local = strings.Clone(network), strings.Clone(local)
	cfg = freezeSetupConfig(cfg)
	a, e := resolveSetupEndpoint(ctx, network, local, &net.Resolver{PreferGo: true})
	if e != nil {
		return nil, e
	}
	if e = validateSetupResolved(network, cfg.Limits, a); e != nil {
		return nil, e
	}
	return listenSetupContext(ctx, network, a, cfg.Socket)
}
