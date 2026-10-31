package sctp

import (
	"net"
	"strconv"
)

func validateSCTPAddr(addr *SCTPAddr, required bool) error {
	if addr == nil {
		if required {
			return &net.AddrError{Err: "missing address"}
		}
		return nil
	}
	if addr.Port < 0 || addr.Port > 65535 {
		return &net.AddrError{Err: "invalid port", Addr: strconv.Itoa(addr.Port)}
	}
	for _, ip := range addr.IPAddrs {
		if len(ip.IP) > 0 && ip.IP.To16() == nil {
			return &net.AddrError{Err: "invalid IP address", Addr: ip.IP.String()}
		}
	}
	return nil
}

func cloneSCTPAddr(addr *SCTPAddr) *SCTPAddr {
	if addr == nil {
		return nil
	}
	copyAddr := &SCTPAddr{Port: addr.Port}
	if addr.IPAddrs != nil {
		copyAddr.IPAddrs = make([]net.IPAddr, len(addr.IPAddrs))
	}
	for i, ip := range addr.IPAddrs {
		copyAddr.IPAddrs[i] = net.IPAddr{IP: append(net.IP(nil), ip.IP...), Zone: ip.Zone}
	}
	return copyAddr
}
