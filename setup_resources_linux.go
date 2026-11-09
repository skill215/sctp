//go:build linux && !386

package sctp

import (
	"net"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// SetupRequirements performs no I/O, DNS, allocation proportional to input or
// kernel capability probing. See docs/bounded-setup.md for the derivation.
func SetupRequirements(cfg SetupConfig) (SetupResourceRequirements, error) {
	if e := validateSetupConfig(cfg); e != nil {
		return SetupResourceRequirements{}, e
	}
	m := MessageOptions{ReceiveInfo: ReceiveInfoSNDRCV, Events: messageEventMask}
	if cfg.Socket.Messages != nil {
		m = *cfg.Socket.Messages
	}
	io, e := MessageIORequirements(m)
	if e != nil {
		return SetupResourceRequirements{}, e
	}
	n, z, d := uint64(cfg.Limits.MaxAddresses), uint64(cfg.Limits.MaxZoneBytes), uint64(cfg.Limits.MaxAddressBytes)
	// One address envelope covers both headers, append capacity, owned IP bytes,
	// raw sockaddrs, split indices, owned zones and transient host/text copies.
	address := 2*uint64(unsafe.Sizeof(SCTPAddr{})) + n*(2*uint64(unsafe.Sizeof(net.IPAddr{}))+16+uint64(unsafe.Sizeof(syscall.RawSockaddrInet6{}))+2*uint64(unsafe.Sizeof(""))) + z + 2*d + 128
	// Logical slots are explicit policy allowances, not sizeof(runtime objects).
	// Channels: 2 gates, close, 2 deadline generations, 2 hook completion channels.
	// Contexts/hooks: 2 operation contexts and 2 AfterFunc registrations.
	fixed := uint64(unsafe.Sizeof(SCTPConn{}) + unsafe.Sizeof(socketIO{}) + unsafe.Sizeof(MessageOptions{}) + unsafe.Sizeof(os.File{}))
	work := 2*uint64(unsafe.Sizeof(socketOperation{})) + uint64(unsafe.Sizeof(Notification{})) + io.PeekScratchBytes + io.ReadScratchBytes + io.WriteScratchBytes
	connection := fixed + work + 7*256 + 4*512 + 2048 + 2048 // file/poller and bounded control/error workspace
	// Legacy nil-Messages calls use a 254-byte ancillary buffer; included even
	// when message mode is selected, avoiding different reserve modes.
	connection += 256
	dialer := uint64(unsafe.Sizeof(Dialer{})+2*unsafe.Sizeof(SetupConfig{})+2*unsafe.Sizeof(MessageOptions{})) + 2*d + 16 + 512
	listener := uint64(unsafe.Sizeof(SCTPListener{})+unsafe.Sizeof(socketIO{})+unsafe.Sizeof(MessageOptions{})+unsafe.Sizeof(os.File{})+unsafe.Sizeof(socketOperation{})) + 6*256 + 2*512 + 2048 + 2048 + 3*address + 2*d
	establish := connection + 6*address + 2048
	return SetupResourceRequirements{LedgerVersion: "bounded-setup-v2/linux/" + runtime.GOARCH, DialerBytes: dialer, ListenerBytes: listener, EstablishBytes: establish, ConnectionBytes: connection, IORequirements: io}, nil
}
