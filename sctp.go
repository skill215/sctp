// Copyright 2019 Wataru Ishida. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sctp

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	SOL_SCTP = 132

	SCTP_BINDX_ADD_ADDR = 0x01
	SCTP_BINDX_REM_ADDR = 0x02

	MSG_NOTIFICATION = 0x8000
)

const (
	SCTP_RTOINFO = iota
	SCTP_ASSOCINFO
	SCTP_INITMSG
	SCTP_NODELAY
	SCTP_AUTOCLOSE
	SCTP_SET_PEER_PRIMARY_ADDR
	SCTP_PRIMARY_ADDR
	SCTP_ADAPTATION_LAYER
	SCTP_DISABLE_FRAGMENTS
	SCTP_PEER_ADDR_PARAMS
	SCTP_DEFAULT_SENT_PARAM
	SCTP_EVENTS
	SCTP_I_WANT_MAPPED_V4_ADDR
	SCTP_MAXSEG
	SCTP_STATUS
	SCTP_GET_PEER_ADDR_INFO
	SCTP_DELAYED_ACK_TIME
	SCTP_DELAYED_ACK  = SCTP_DELAYED_ACK_TIME
	SCTP_DELAYED_SACK = SCTP_DELAYED_ACK_TIME

	SCTP_SOCKOPT_BINDX_ADD     = 100
	SCTP_SOCKOPT_BINDX_REM     = 101
	SCTP_SOCKOPT_PEELOFF       = 102
	SCTP_GET_PEER_ADDRS        = 108
	SCTP_GET_LOCAL_ADDRS       = 109
	SCTP_SOCKOPT_CONNECTX      = 110
	SCTP_SOCKOPT_CONNECTX3     = 111
	SCTP_SOCKOPT_PEELOFF_FLAGS = 122
)

const (
	SCTP_EVENT_DATA_IO = 1 << iota
	SCTP_EVENT_ASSOCIATION
	SCTP_EVENT_ADDRESS
	SCTP_EVENT_SEND_FAILURE
	SCTP_EVENT_PEER_ERROR
	SCTP_EVENT_SHUTDOWN
	SCTP_EVENT_PARTIAL_DELIVERY
	SCTP_EVENT_ADAPTATION_LAYER
	SCTP_EVENT_AUTHENTICATION
	SCTP_EVENT_SENDER_DRY

	SCTP_EVENT_ALL = SCTP_EVENT_DATA_IO | SCTP_EVENT_ASSOCIATION | SCTP_EVENT_ADDRESS | SCTP_EVENT_SEND_FAILURE | SCTP_EVENT_PEER_ERROR | SCTP_EVENT_SHUTDOWN | SCTP_EVENT_PARTIAL_DELIVERY | SCTP_EVENT_ADAPTATION_LAYER | SCTP_EVENT_AUTHENTICATION | SCTP_EVENT_SENDER_DRY
)

type (
	SCTPNotificationType int
	SCTPAssocID          int32
)

const (
	SCTP_SN_TYPE_BASE = SCTPNotificationType(iota + (1 << 15))
	SCTP_ASSOC_CHANGE
	SCTP_PEER_ADDR_CHANGE
	SCTP_SEND_FAILED
	SCTP_REMOTE_ERROR
	SCTP_SHUTDOWN_EVENT
	SCTP_PARTIAL_DELIVERY_EVENT
	SCTP_ADAPTATION_INDICATION
	SCTP_AUTHENTICATION_INDICATION
	SCTP_SENDER_DRY_EVENT
)

type NotificationHandler func([]byte) error

type EventSubscribe struct {
	DataIO          uint8
	Association     uint8
	Address         uint8
	SendFailure     uint8
	PeerError       uint8
	Shutdown        uint8
	PartialDelivery uint8
	AdaptationLayer uint8
	Authentication  uint8
	SenderDry       uint8
}

const (
	SCTP_CMSG_INIT = iota
	SCTP_CMSG_SNDRCV
	SCTP_CMSG_SNDINFO
	SCTP_CMSG_RCVINFO
	SCTP_CMSG_NXTINFO
)

const (
	SCTP_UNORDERED = 1 << iota
	SCTP_ADDR_OVER
	SCTP_ABORT
	SCTP_SACK_IMMEDIATELY
	SCTP_EOF = 0x200 // Linux MSG_FIN; not the next iota bit.
)

const (
	SCTP_MAX_STREAM = 0xffff
)

type InitMsg struct {
	NumOstreams    uint16
	MaxInstreams   uint16
	MaxAttempts    uint16
	MaxInitTimeout uint16
}

// SackTimer Parameters defined in RFC 6458 8.1.19 - SCTP_DELAYED_SACK Delayed Sack Timer sack_timeout
type SackTimer struct {
	AssocID       SCTPAssocID
	SackDelay     uint32
	SackFrequency uint32
}

type PeerState int32

const (
	SCTP_INACTIVE    PeerState = 0
	SCTP_PF          PeerState = 1
	SCTP_ACTIVE      PeerState = 2
	SCTP_UNCONFIRMED PeerState = 3
	SCTP_UNKNOWN     PeerState = 0xffff
)

// PeerAddrinfo Parameters defined in RFC 6458 8.2.2 - Peer Address Information (SCTP_GET_PEER_ADDR_INFO)
type PeerAddrinfo struct {
	AssocID SCTPAssocID
	Address [128]byte // Raw sockaddr_storage; use SCTPGetPrimaryPeerAddr to decode the primary address.
	State   PeerState
	CWND    uint32
	SRTT    uint32
	RTO     uint32
	MTU     uint32
}

type StatusState int32

const (
	SCTP_EMPTY             StatusState = 0
	SCTP_CLOSED            StatusState = 1
	SCTP_COOKIE_WAIT       StatusState = 2
	SCTP_COOKIE_ECHOED     StatusState = 3
	SCTP_ESTABLISHED       StatusState = 4
	SCTP_SHUTDOWN_PENDING  StatusState = 5
	SCTP_SHUTDOWN_SENT     StatusState = 6
	SCTP_SHUTDOWN_RECEIVED StatusState = 7
	SCTP_SHUTDOWN_ACK_SENT StatusState = 8

	// Deprecated: Linux SCTP_STATUS does not return bound or listen states.
	SCTP_BOUND  StatusState = -1
	SCTP_LISTEN StatusState = -2
)

// Status Parameters defined in RFC 6458 8.2.1 - Association Status (SCTP_STATUS)
type Status struct {
	AssocID            SCTPAssocID
	State              StatusState
	RWND               uint32
	Unackdata          uint16
	Penddata           uint16
	Instreams          uint16
	Ostreams           uint16
	FragmentationPoint uint32
	PrimaryPeerAddr    PeerAddrinfo
}

type SndRcvInfo struct {
	Stream uint16
	SSN    uint16
	Flags  uint16
	_      uint16
	// This parameter’s endianness is not specified in the SCTP standards, but as is the case with Wireshark, it seems
	// that it is generally treated as network byte order.
	//
	// IANA defines them as integer values, hence we would assume that our users would use the trouble-free host
	PPID    uint32
	Context uint32
	TTL     uint32
	TSN     uint32
	CumTSN  uint32
	AssocID int32
}

type SndInfo struct {
	SID     uint16
	Flags   uint16
	PPID    uint32
	Context uint32
	AssocID int32
}

type GetAddrsOld struct {
	AssocID int32
	AddrNum int32
	Addrs   uintptr
}

type NotificationHeader struct {
	Type   uint16
	Flags  uint16
	Length uint32
}

type SCTPState uint16

const (
	SCTP_COMM_UP = SCTPState(iota)
	SCTP_COMM_LOST
	SCTP_RESTART
	SCTP_SHUTDOWN_COMP
	SCTP_CANT_STR_ASSOC
)

var nativeEndian = binary.NativeEndian
var nativeLittleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

const sndRcvInfoSize = unsafe.Sizeof(SndRcvInfo{})

func htons(h uint16) uint16 {
	if nativeLittleEndian {
		return bits.ReverseBytes16(h)
	}
	return h
}

var ntohs = htons

func htonl(h uint32) uint32 {
	if nativeLittleEndian {
		return bits.ReverseBytes32(h)
	}
	return h
}

var ntohl = htonl

// setInitOpts sets options for an SCTP association initialization
// see https://tools.ietf.org/html/rfc4960#page-25
func setInitOpts(fd int, options InitMsg) error {
	optlen := unsafe.Sizeof(options)
	_, _, err := setsockopt(fd, SCTP_INITMSG, uintptr(unsafe.Pointer(&options)), uintptr(optlen))
	return err
}

func setNumOstreams(fd, num int) error {
	return setInitOpts(fd, InitMsg{NumOstreams: uint16(num)})
}

type SCTPAddr struct {
	IPAddrs []net.IPAddr
	Port    int
}

func (a *SCTPAddr) ToRawSockAddrBuf() []byte {
	p := htons(uint16(a.Port))
	if len(a.IPAddrs) == 0 { // if a.IPAddrs list is empty - fall back to IPv4 zero addr
		s := syscall.RawSockaddrInet4{
			Family: syscall.AF_INET,
			Port:   p,
		}
		copy(s.Addr[:], net.IPv4zero)
		return toBuf(s)
	}
	buf := []byte{}
	for _, ip := range a.IPAddrs {
		ipBytes := ip.IP
		if len(ipBytes) == 0 {
			ipBytes = net.IPv4zero
		}
		if ip4 := ipBytes.To4(); ip4 != nil {
			s := syscall.RawSockaddrInet4{
				Family: syscall.AF_INET,
				Port:   p,
			}
			copy(s.Addr[:], ip4)
			buf = append(buf, toBuf(s)...)
		} else {
			var scopeid uint32
			// Interface names may be numeric; preserve name precedence before
			// treating an otherwise unknown zone as an interface index.
			if ifi, err := net.InterfaceByName(ip.Zone); err == nil {
				scopeid = uint32(ifi.Index)
			} else if numeric, err := strconv.ParseUint(ip.Zone, 10, 32); err == nil {
				scopeid = uint32(numeric)
			}
			s := syscall.RawSockaddrInet6{
				Family:   syscall.AF_INET6,
				Port:     p,
				Scope_id: scopeid,
			}
			copy(s.Addr[:], ipBytes)
			buf = append(buf, toBuf(s)...)
		}
	}
	return buf
}

func (a *SCTPAddr) String() string {
	var b strings.Builder

	for n, i := range a.IPAddrs {
		if i.IP.To4() != nil {
			b.WriteString(i.String())
		} else if i.IP.To16() != nil {
			b.WriteRune('[')
			b.WriteString(i.String())
			b.WriteRune(']')
		}
		if n < len(a.IPAddrs)-1 {
			b.WriteRune('/')
		}
	}
	b.WriteRune(':')
	b.WriteString(strconv.Itoa(a.Port))
	return b.String()
}

func (a *SCTPAddr) Network() string { return "sctp" }

func normalizeNetwork(network string) (string, error) {
	switch network {
	case "":
		return "sctp", nil
	case "sctp", "sctp4", "sctp6":
		return network, nil
	default:
		return "", fmt.Errorf("invalid net: %s", network)
	}
}

func ResolveSCTPAddr(network, addrs string) (*SCTPAddr, error) {
	network, err := normalizeNetwork(network)
	if err != nil {
		return nil, err
	}
	tcpnet := "tcp"
	switch network {
	case "sctp4":
		tcpnet = "tcp4"
	case "sctp6":
		tcpnet = "tcp6"
	}
	elems := strings.Split(addrs, "/")
	if len(elems) == 0 {
		return nil, fmt.Errorf("invalid input: %s", addrs)
	}
	ipaddrs := make([]net.IPAddr, 0, len(elems))
	for _, e := range elems[:len(elems)-1] {
		tcpa, err := net.ResolveTCPAddr(tcpnet, e+":")
		if err != nil {
			return nil, err
		}
		ipaddrs = append(ipaddrs, net.IPAddr{IP: tcpa.IP, Zone: tcpa.Zone})
	}
	tcpa, err := net.ResolveTCPAddr(tcpnet, elems[len(elems)-1])
	if err != nil {
		return nil, err
	}
	if tcpa.IP != nil {
		ipaddrs = append(ipaddrs, net.IPAddr{IP: tcpa.IP, Zone: tcpa.Zone})
	} else {
		ipaddrs = nil
	}
	return &SCTPAddr{
		IPAddrs: ipaddrs,
		Port:    tcpa.Port,
	}, nil
}

func SCTPConnect(fd int, addr *SCTPAddr) (int, error) {
	buf := addr.ToRawSockAddrBuf()
	param := GetAddrsOld{
		AddrNum: int32(len(buf)),
		Addrs:   uintptr(uintptr(unsafe.Pointer(&buf[0]))),
	}
	optlen := uint32(unsafe.Sizeof(param))
	_, _, err := getsockopt(fd, SCTP_SOCKOPT_CONNECTX3, uintptr(unsafe.Pointer(&param)), &optlen)
	if err == nil {
		return int(param.AssocID), nil
	} else if err != syscall.ENOPROTOOPT {
		return 0, err
	}
	r0, _, err := setsockopt(fd, SCTP_SOCKOPT_CONNECTX, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return int(r0), err
}

func SCTPBind(fd int, addr *SCTPAddr, flags int) error {
	var option uintptr
	switch flags {
	case SCTP_BINDX_ADD_ADDR:
		option = SCTP_SOCKOPT_BINDX_ADD
	case SCTP_BINDX_REM_ADDR:
		option = SCTP_SOCKOPT_BINDX_REM
	default:
		return syscall.EINVAL
	}

	buf := addr.ToRawSockAddrBuf()
	_, _, err := setsockopt(fd, option, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return err
}

type SCTPConn struct {
	_fd                 atomic.Int32
	file                *os.File
	io                  *socketIO
	initErr             error
	eventsMu            sync.Mutex
	singleAssociation   bool // immutable after construction; includes library peeloffs
	notificationHandler NotificationHandler
}

// socketIO serializes each polling direction so an operation's temporary
// socket timeout cannot change another operation's poll deadline.
type socketIO struct {
	file      *os.File
	readGate  chan struct{}
	writeGate chan struct{}
	closing   chan struct{}
	closeOnce sync.Once
}

func (c *SCTPConn) fd() int {
	return int(c._fd.Load())
}

// NewSCTPConn takes ownership of fd and configures it for runtime polling.
// Callers must use Close to release it and must not close fd independently.
func NewSCTPConn(fd int, handler NotificationHandler) *SCTPConn {
	return newSCTPConn(fd, handler)
}

func (c *SCTPConn) withFD(f func(int) error) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var opErr error
	if err := raw.Control(func(fd uintptr) { opErr = f(int(fd)) }); err != nil {
		return err
	}
	return opErr
}

//go:uintptrescapes
func (c *SCTPConn) getsockopt(optname, optval uintptr, optlen *uint32) (r0, r1 uintptr, err error) {
	err = c.withFD(func(fd int) error {
		var opErr error
		r0, r1, opErr = getsockopt(fd, optname, optval, optlen)
		return opErr
	})
	return
}

//go:uintptrescapes
func (c *SCTPConn) setsockopt(optname, optval, optlen uintptr) (r0, r1 uintptr, err error) {
	err = c.withFD(func(fd int) error {
		var opErr error
		r0, r1, opErr = setsockopt(fd, optname, optval, optlen)
		return opErr
	})
	return
}

func (c *SCTPConn) Write(b []byte) (int, error) {
	return c.SCTPWrite(b, nil)
}

func (c *SCTPConn) Read(b []byte) (int, error) {
	n, _, err := c.SCTPRead(b)
	if n < 0 {
		n = 0
	}
	return n, err
}

func (c *SCTPConn) SetInitMsg(numOstreams, maxInstreams, maxAttempts, maxInitTimeout int) error {
	options := InitMsg{
		NumOstreams:    uint16(numOstreams),
		MaxInstreams:   uint16(maxInstreams),
		MaxAttempts:    uint16(maxAttempts),
		MaxInitTimeout: uint16(maxInitTimeout),
	}
	_, _, err := c.setsockopt(SCTP_INITMSG, uintptr(unsafe.Pointer(&options)), unsafe.Sizeof(options))
	return err
}

func (c *SCTPConn) SubscribeEvents(flags int) error {
	if c == nil {
		return syscall.EBADF
	}
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	return c.subscribeEvents(flags)
}

func (c *SCTPConn) subscribeEvents(flags int) error {
	var d, a, ad, sf, p, sh, pa, ada, au, se uint8
	if flags&SCTP_EVENT_DATA_IO > 0 {
		d = 1
	}
	if flags&SCTP_EVENT_ASSOCIATION > 0 {
		a = 1
	}
	if flags&SCTP_EVENT_ADDRESS > 0 {
		ad = 1
	}
	if flags&SCTP_EVENT_SEND_FAILURE > 0 {
		sf = 1
	}
	if flags&SCTP_EVENT_PEER_ERROR > 0 {
		p = 1
	}
	if flags&SCTP_EVENT_SHUTDOWN > 0 {
		sh = 1
	}
	if flags&SCTP_EVENT_PARTIAL_DELIVERY > 0 {
		pa = 1
	}
	if flags&SCTP_EVENT_ADAPTATION_LAYER > 0 {
		ada = 1
	}
	if flags&SCTP_EVENT_AUTHENTICATION > 0 {
		au = 1
	}
	if flags&SCTP_EVENT_SENDER_DRY > 0 {
		se = 1
	}
	param := EventSubscribe{
		DataIO:          d,
		Association:     a,
		Address:         ad,
		SendFailure:     sf,
		PeerError:       p,
		Shutdown:        sh,
		PartialDelivery: pa,
		AdaptationLayer: ada,
		Authentication:  au,
		SenderDry:       se,
	}
	optlen := unsafe.Sizeof(param)
	_, _, err := c.setsockopt(SCTP_EVENTS, uintptr(unsafe.Pointer(&param)), uintptr(optlen))
	return err
}

func (c *SCTPConn) SubscribedEvents() (int, error) {
	if c == nil {
		return 0, syscall.EBADF
	}
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	return c.subscribedEvents()
}

func (c *SCTPConn) enableDataIO() error {
	if c == nil {
		return syscall.EBADF
	}
	c.eventsMu.Lock()
	defer c.eventsMu.Unlock()
	flags, err := c.subscribedEvents()
	if err != nil {
		return err
	}
	return c.subscribeEvents(flags | SCTP_EVENT_DATA_IO)
}

func (c *SCTPConn) subscribedEvents() (int, error) {
	param := EventSubscribe{}
	optlen := uint32(unsafe.Sizeof(param))
	_, _, err := c.getsockopt(SCTP_EVENTS, uintptr(unsafe.Pointer(&param)), &optlen)
	if err != nil {
		return 0, err
	}
	var flags int
	if param.DataIO > 0 {
		flags |= SCTP_EVENT_DATA_IO
	}
	if param.Association > 0 {
		flags |= SCTP_EVENT_ASSOCIATION
	}
	if param.Address > 0 {
		flags |= SCTP_EVENT_ADDRESS
	}
	if param.SendFailure > 0 {
		flags |= SCTP_EVENT_SEND_FAILURE
	}
	if param.PeerError > 0 {
		flags |= SCTP_EVENT_PEER_ERROR
	}
	if param.Shutdown > 0 {
		flags |= SCTP_EVENT_SHUTDOWN
	}
	if param.PartialDelivery > 0 {
		flags |= SCTP_EVENT_PARTIAL_DELIVERY
	}
	if param.AdaptationLayer > 0 {
		flags |= SCTP_EVENT_ADAPTATION_LAYER
	}
	if param.Authentication > 0 {
		flags |= SCTP_EVENT_AUTHENTICATION
	}
	if param.SenderDry > 0 {
		flags |= SCTP_EVENT_SENDER_DRY
	}
	return flags, nil
}

func (c *SCTPConn) SetDefaultSentParam(info *SndRcvInfo) error {
	if info == nil {
		return syscall.EINVAL
	}
	copyInfo := *info
	copyInfo.PPID = htonl(copyInfo.PPID)
	_, _, err := c.setsockopt(SCTP_DEFAULT_SENT_PARAM, uintptr(unsafe.Pointer(&copyInfo)), unsafe.Sizeof(copyInfo))
	return err
}

func (c *SCTPConn) GetDefaultSentParam() (*SndRcvInfo, error) {
	info := &SndRcvInfo{}
	optlen := uint32(unsafe.Sizeof(*info))
	_, _, err := c.getsockopt(SCTP_DEFAULT_SENT_PARAM, uintptr(unsafe.Pointer(info)), &optlen)
	if err == nil {
		info.PPID = ntohl(info.PPID)
	}
	return info, err
}

func (c *SCTPConn) SetSackTimer(timer *SackTimer) error { // SackTimer
	optlen := unsafe.Sizeof(*timer)
	_, _, err := c.setsockopt(SCTP_DELAYED_SACK, uintptr(unsafe.Pointer(timer)), optlen)
	return err
}

func (c *SCTPConn) GetSackTimer() (*SackTimer, error) { // SackTimer
	timer := &SackTimer{}
	optlen := uint32(unsafe.Sizeof(*timer))
	_, _, err := c.getsockopt(
		SCTP_DELAYED_SACK,
		uintptr(unsafe.Pointer(timer)),
		&optlen,
	)
	return timer, err
}

func (c *SCTPConn) GetStatus() (*Status, error) { // Status
	sctpStatus := &Status{}
	optlen := uint32(unsafe.Sizeof(*sctpStatus))
	_, _, err := c.getsockopt(
		SCTP_STATUS,
		uintptr(unsafe.Pointer(sctpStatus)),
		&optlen,
	)
	return sctpStatus, err
}

// Getsockopt invokes a Linux SCTP socket option. optlen points to a uint32
// socklen_t, including on 64-bit architectures.
//
//go:uintptrescapes
func (c *SCTPConn) Getsockopt(optname, optval, optlen uintptr) (r0, r1 uintptr, err error) {
	// Keep the public uintptr API as raw syscall arguments. Converting an
	// incoming uintptr back into a Go pointer would violate checkptr rules.
	err = c.withFD(func(fd int) error {
		var opErr error
		r0, r1, opErr = getSocketOption(fd, SOL_SCTP, optname, optval, optlen)
		return opErr
	})
	return
}

//go:uintptrescapes
func (c *SCTPConn) Setsockopt(optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	return c.setsockopt(optname, optval, optlen)
}

func resolveFromRawAddr(buf []byte, n int) (*SCTPAddr, error) {
	const size4 = int(unsafe.Sizeof(syscall.RawSockaddrInet4{}))
	const size6 = int(unsafe.Sizeof(syscall.RawSockaddrInet6{}))
	if n < 0 || n > len(buf)/size4 {
		return nil, syscall.EINVAL
	}
	addr := &SCTPAddr{
		IPAddrs: make([]net.IPAddr, 0, n),
	}
	for i := range n {
		if len(buf) < 2 {
			return nil, syscall.EINVAL
		}
		var ip net.IPAddr
		var size int
		switch family := nativeEndian.Uint16(buf[:2]); family {
		case syscall.AF_INET:
			size = size4
			if len(buf) < size {
				return nil, syscall.EINVAL
			}
			ip.IP = append(net.IP(nil), buf[4:8]...)
		case syscall.AF_INET6:
			size = size6
			if len(buf) < size {
				return nil, syscall.EINVAL
			}
			ip.IP = append(net.IP(nil), buf[8:24]...)
			if scopeID := nativeEndian.Uint32(buf[24:28]); scopeID != 0 {
				ip.Zone = strconv.FormatUint(uint64(scopeID), 10)
				if ifi, err := net.InterfaceByIndex(int(scopeID)); err == nil {
					ip.Zone = ifi.Name
				}
			}
		default:
			return nil, fmt.Errorf("unknown address family: %d", family)
		}
		if i == 0 {
			addr.Port = int(binary.BigEndian.Uint16(buf[2:4]))
		}
		addr.IPAddrs = append(addr.IPAddrs, ip)
		buf = buf[size:]
	}
	return addr, nil
}

func sctpGetAddrs(fd, id, optname int) (*SCTPAddr, error) {

	type getaddrs struct {
		assocId int32
		addrNum uint32
		addrs   [4096]byte
	}
	param := getaddrs{
		assocId: int32(id),
	}
	optlen := uint32(unsafe.Sizeof(param))
	_, _, err := getsockopt(fd, uintptr(optname), uintptr(unsafe.Pointer(&param)), &optlen)
	if err != nil {
		return nil, err
	}
	// Linux's local-address option returns only the address bytes in optlen;
	// the peer-address option also counts the sctp_getaddrs header.
	if optname == SCTP_GET_PEER_ADDRS {
		headerLen := uint32(unsafe.Offsetof(param.addrs))
		if optlen < headerLen {
			return nil, syscall.EINVAL
		}
		optlen -= headerLen
	}
	if optlen > uint32(len(param.addrs)) {
		return nil, syscall.EINVAL
	}
	return resolveFromRawAddr(param.addrs[:optlen], int(param.addrNum))
}

func (c *SCTPConn) SCTPGetPrimaryPeerAddr() (*SCTPAddr, error) {

	type sctpGetSetPrim struct {
		assocId int32
		addrs   [128]byte
	}
	param := sctpGetSetPrim{
		assocId: int32(0),
	}
	optlen := uint32(unsafe.Sizeof(param))
	_, _, err := c.getsockopt(SCTP_PRIMARY_ADDR, uintptr(unsafe.Pointer(&param)), &optlen)
	if err != nil {
		return nil, err
	}
	return resolveFromRawAddr(param.addrs[:], 1)
}

func (c *SCTPConn) getAddrs(id, optname int) (*SCTPAddr, error) {
	var addr *SCTPAddr
	err := c.withFD(func(fd int) error {
		var err error
		addr, err = sctpGetAddrs(fd, id, optname)
		return err
	})
	return addr, err
}

func (c *SCTPConn) SCTPLocalAddr(id int) (*SCTPAddr, error) {
	return c.getAddrs(id, SCTP_GET_LOCAL_ADDRS)
}

func (c *SCTPConn) SCTPRemoteAddr(id int) (*SCTPAddr, error) {
	return c.getAddrs(id, SCTP_GET_PEER_ADDRS)
}

func (c *SCTPConn) LocalAddr() net.Addr {
	addr, err := c.SCTPLocalAddr(0)
	if err != nil {
		return nil
	}
	return addr
}

func (c *SCTPConn) RemoteAddr() net.Addr {
	addr, err := c.SCTPRemoteAddr(0)
	if err != nil {
		return nil
	}
	return addr
}

func (c *SCTPConn) PeelOff(id int) (*SCTPConn, error) {
	var peeledFD int
	err := c.withFD(func(fd int) error {
		var err error
		peeledFD, err = peelOffSocket(fd, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	conn := NewSCTPConn(peeledFD, c.notificationHandler)
	conn.singleAssociation = true
	if conn.initErr != nil {
		conn.Close()
		return nil, conn.initErr
	}
	return conn, nil
}

func (c *SCTPConn) SetDeadline(t time.Time) error {
	return syscall.EOPNOTSUPP
}

func (c *SCTPConn) SetReadDeadline(t time.Time) error {
	return syscall.EOPNOTSUPP
}

func (c *SCTPConn) SetWriteDeadline(t time.Time) error {
	return syscall.EOPNOTSUPP
}

type SCTPListener struct {
	fd                  int
	file                *os.File
	io                  *socketIO
	notificationHandler NotificationHandler
}

func (ln *SCTPListener) Addr() net.Addr {
	if ln == nil || ln.file == nil {
		return nil
	}
	raw, err := ln.file.SyscallConn()
	if err != nil {
		return nil
	}
	var laddr *SCTPAddr
	err = raw.Control(func(fd uintptr) {
		laddr, _ = sctpGetAddrs(int(fd), 0, SCTP_GET_LOCAL_ADDRS)
	})
	if err != nil || laddr == nil {
		return nil
	}
	return laddr
}

type SCTPSndRcvInfoWrappedConn struct {
	conn    *SCTPConn
	initErr error
}

func NewSCTPSndRcvInfoWrappedConn(conn *SCTPConn) *SCTPSndRcvInfoWrappedConn {
	return &SCTPSndRcvInfoWrappedConn{conn: conn, initErr: conn.enableDataIO()}
}

func (c *SCTPSndRcvInfoWrappedConn) Write(b []byte) (int, error) {
	if c.initErr != nil {
		return 0, c.initErr
	}
	if len(b) < int(sndRcvInfoSize) {
		return 0, syscall.EINVAL
	}
	var info SndRcvInfo
	if err := decodeNative(b[:sndRcvInfoSize], &info); err != nil {
		return 0, err
	}
	n, err := c.conn.SCTPWrite(b[sndRcvInfoSize:], &info)
	if n <= 0 && err != nil {
		return 0, err
	}
	return n + int(sndRcvInfoSize), err
}

func (c *SCTPSndRcvInfoWrappedConn) Read(b []byte) (int, error) {
	if c.initErr != nil {
		return 0, c.initErr
	}
	if len(b) <= int(sndRcvInfoSize) {
		return 0, syscall.EINVAL
	}
	n, info, err := c.conn.SCTPRead(b[sndRcvInfoSize:])
	return wrappedReadResult(b, n, info, err)
}

func wrappedReadResult(b []byte, n int, info *SndRcvInfo, err error) (int, error) {
	if n <= 0 && err != nil {
		return 0, err
	}
	clear(b[:sndRcvInfoSize])
	if info != nil {
		copy(b, toBuf(info))
	}
	return n + int(sndRcvInfoSize), err
}

func (c *SCTPSndRcvInfoWrappedConn) Close() error {
	return c.conn.Close()
}

func (c *SCTPSndRcvInfoWrappedConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *SCTPSndRcvInfoWrappedConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *SCTPSndRcvInfoWrappedConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *SCTPSndRcvInfoWrappedConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *SCTPSndRcvInfoWrappedConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *SCTPSndRcvInfoWrappedConn) SetWriteBuffer(bytes int) error {
	return c.conn.SetWriteBuffer(bytes)
}

func (c *SCTPSndRcvInfoWrappedConn) GetWriteBuffer() (int, error) {
	return c.conn.GetWriteBuffer()
}

func (c *SCTPSndRcvInfoWrappedConn) SetReadBuffer(bytes int) error {
	return c.conn.SetReadBuffer(bytes)
}

func (c *SCTPSndRcvInfoWrappedConn) GetReadBuffer() (int, error) {
	return c.conn.GetReadBuffer()
}

// SocketConfig contains options for the SCTP socket.
type SocketConfig struct {
	// If Control is not nil it is called after the socket is created but before
	// it is bound or connected.
	Control func(network, address string, c syscall.RawConn) error
	// NotificationHandler defines actions taken on received notifications when MSG_NOTIFICATION flag is set.
	NotificationHandler NotificationHandler
	// InitMsg is the options to send in the initial SCTP message
	InitMsg InitMsg
}

func (cfg *SocketConfig) Listen(net string, laddr *SCTPAddr) (*SCTPListener, error) {
	return listenSCTPExtConfig(net, laddr, cfg.InitMsg, cfg.Control, cfg.NotificationHandler)
}

func (cfg *SocketConfig) Dial(net string, laddr, raddr *SCTPAddr) (*SCTPConn, error) {
	return dialSCTPExtConfig(net, laddr, raddr, cfg.InitMsg, cfg.Control, cfg.NotificationHandler)
}
