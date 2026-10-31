//go:build linux && !386

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
	"io"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

//go:uintptrescapes
func setsockopt(fd int, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	// FIXME: syscall.SYS_SETSOCKOPT is undefined on 386
	r0, r1, errno := syscall.Syscall6(syscall.SYS_SETSOCKOPT,
		uintptr(fd),
		SOL_SCTP,
		optname,
		optval,
		optlen,
		0)
	if errno != 0 {
		return r0, r1, errno
	}
	return r0, r1, nil
}

//go:uintptrescapes
func getsockopt(fd int, optname, optval uintptr, optlen *uint32) (uintptr, uintptr, error) {
	return getSocketOption(fd, SOL_SCTP, optname, optval, uintptr(unsafe.Pointer(optlen)))
}

//go:uintptrescapes
func getSocketOption(fd int, level, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	// Linux socklen_t is 32 bits on every supported architecture.
	// FIXME: syscall.SYS_GETSOCKOPT is undefined on 386
	r0, r1, errno := syscall.Syscall6(syscall.SYS_GETSOCKOPT,
		uintptr(fd),
		level,
		optname,
		optval,
		optlen,
		0)
	if errno != 0 {
		return r0, r1, errno
	}
	return r0, r1, nil
}

// socketFile owns fd. Non-blocking syscalls let the runtime poller wake all
// readers, writers and acceptors on Close, while pinning fd during callbacks.
func socketFile(fd int) (*os.File, error) {
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "sctp")
	if file == nil {
		return nil, syscall.EBADF
	}
	return file, nil
}

func newSCTPConn(fd int, handler NotificationHandler) *SCTPConn {
	file, err := socketFile(fd)
	if err != nil {
		syscall.Close(fd)
		fd = -1
	}
	conn := &SCTPConn{file: file, io: newSocketIO(file), initErr: err, notificationHandler: handler}
	conn._fd.Store(int32(fd))
	if err == nil {
		kind, kindErr := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_TYPE)
		protocol, protocolErr := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
		if kindErr == nil && protocolErr == nil && protocol == syscall.IPPROTO_SCTP {
			conn.singleAssociation = kind == syscall.SOCK_STREAM
			if kind == syscall.SOCK_SEQPACKET {
				conn.singleAssociation = associationReadError(fd) == nil
			}
		}
	}
	return conn
}

func (c *SCTPConn) socketRaw() (*socketIO, error) {
	if c == nil {
		return nil, syscall.EBADF
	}
	if c.initErr != nil {
		return nil, c.initErr
	}
	if c.fd() < 0 {
		return nil, net.ErrClosed
	}
	if c.file == nil {
		return nil, syscall.EBADF
	}
	return c.io, nil
}

func (c *SCTPConn) SyscallConn() (syscall.RawConn, error) {
	if c == nil || c.fd() < 0 {
		return nil, syscall.EINVAL
	}
	raw, err := c.socketRaw()
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *SCTPConn) SCTPWrite(b []byte, info *SndRcvInfo) (int, error) {
	var cbuf []byte
	if info != nil {
		// Fix PPID to network byte order
		copyInfo := *info
		copyInfo.PPID = htonl(copyInfo.PPID)
		cmsgBuf := toBuf(&copyInfo)
		hdr := &syscall.Cmsghdr{
			Level: syscall.IPPROTO_SCTP,
			Type:  SCTP_CMSG_SNDRCV,
		}

		// bitwidth of hdr.Len is platform-specific,
		// so we use hdr.SetLen() rather than directly setting hdr.Len
		hdr.SetLen(syscall.CmsgSpace(len(cmsgBuf)))
		cbuf = append(toBuf(hdr), cmsgBuf...)
	}
	raw, err := c.socketRaw()
	if err != nil {
		return 0, err
	}
	var n int
	var writeErr error
	err = raw.Write(func(fd uintptr) bool {
		for {
			if len(b) == 0 {
				n, writeErr = sendmsgZero(int(fd), cbuf)
			} else {
				n, writeErr = syscall.SendmsgN(int(fd), b, cbuf, nil, 0)
			}
			if writeErr != syscall.EINTR {
				return writeErr != syscall.EAGAIN
			}
		}
	})
	if err != nil {
		if c.fd() < 0 {
			return 0, net.ErrClosed
		}
		return 0, err
	}
	return max(n, 0), writeErr
}

func parseSndRcvInfo(b []byte) (*SndRcvInfo, error) {
	msgs, err := syscall.ParseSocketControlMessage(b)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if m.Header.Level == syscall.IPPROTO_SCTP {
			switch m.Header.Type {
			case SCTP_CMSG_SNDRCV:
				if len(m.Data) < int(sndRcvInfoSize) {
					return nil, syscall.EINVAL
				}
				dst := new(SndRcvInfo)
				if err := decodeNative(m.Data[:sndRcvInfoSize], dst); err != nil {
					return nil, err
				}
				// Fix PPID to host byte order
				dst.PPID = ntohl(dst.PPID)
				return dst, nil
			}
		}
	}
	return nil, nil
}

func (c *SCTPConn) SCTPRead(b []byte) (int, *SndRcvInfo, error) {
	raw, err := c.socketRaw()
	if err != nil {
		return 0, nil, err
	}
	if len(b) == 0 {
		return 0, nil, nil
	}
	deadline, err := raw.deadline(true)
	if err != nil {
		if c.fd() < 0 {
			return 0, nil, net.ErrClosed
		}
		return 0, nil, err
	}
	oob := make([]byte, 254)
	for {
		var n, oobn, recvflags int
		var readErr error
		err := raw.pollDeadline(true, deadline, func(fd uintptr) bool {
			for {
				n, oobn, recvflags, _, readErr = syscall.Recvmsg(int(fd), b, oob, 0)
				if readErr != syscall.EINTR {
					if readErr == syscall.EAGAIN && c.singleAssociation {
						readErr = associationReadError(int(fd))
						if readErr == nil {
							return false
						}
						if readErr == syscall.ENOTCONN {
							// Shutdown or data can arrive between the first
							// EAGAIN and the status query. Drain that result
							// before reporting an absent association.
							for {
								n, oobn, recvflags, _, readErr = syscall.Recvmsg(int(fd), b, oob, 0)
								if readErr != syscall.EINTR {
									break
								}
							}
							if readErr == syscall.EAGAIN {
								readErr = syscall.ENOTCONN
							}
						}
						return true // preserve every status-query error
					}
					return readErr != syscall.EAGAIN
				}
			}
		})
		if err != nil {
			if c.fd() < 0 {
				return 0, nil, net.ErrClosed
			}
			return 0, nil, err
		}
		if readErr != nil {
			return 0, nil, readErr
		}

		if n == 0 && oobn == 0 {
			return 0, nil, io.EOF
		}

		if recvflags&MSG_NOTIFICATION > 0 && c.notificationHandler != nil {
			if err := c.notificationHandler(b[:n]); err != nil {
				return 0, nil, err
			}
		} else {
			var info *SndRcvInfo
			if oobn > 0 {
				info, err = parseSndRcvInfo(oob[:oobn])
			}
			return n, info, err
		}
	}
}

func (c *SCTPConn) Close() error {
	if c != nil {
		fd := c._fd.Swap(-1)
		if fd >= 0 && c.file != nil {
			return c.io.Close()
		}
	}
	return syscall.EBADF
}

func (c *SCTPConn) SetWriteBuffer(bytes int) error {
	return c.withFD(func(fd int) error { return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, bytes) })
}

func (c *SCTPConn) GetWriteBuffer() (size int, err error) {
	err = c.withFD(func(fd int) error {
		var err error
		size, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF)
		return err
	})
	return
}

func (c *SCTPConn) SetReadBuffer(bytes int) error {
	return c.withFD(func(fd int) error { return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, bytes) })
}

func (c *SCTPConn) GetReadBuffer() (size int, err error) {
	err = c.withFD(func(fd int) error {
		var err error
		size, err = syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF)
		return err
	})
	return
}

// ListenSCTP - start listener on specified address/port
func ListenSCTP(net string, laddr *SCTPAddr) (*SCTPListener, error) {
	return ListenSCTPExt(net, laddr, InitMsg{NumOstreams: SCTP_MAX_STREAM})
}

// ListenSCTPExt - start listener on specified address/port with given SCTP options
func ListenSCTPExt(network string, laddr *SCTPAddr, options InitMsg) (*SCTPListener, error) {
	return listenSCTPExtConfig(network, laddr, options, nil, nil)
}

// listenSCTPExtConfig - start listener on specified address/port with given SCTP options and socket configuration
func listenSCTPExtConfig(network string, laddr *SCTPAddr, options InitMsg, control func(network, address string, c syscall.RawConn) error, notificationHandler NotificationHandler) (*SCTPListener, error) {
	network, err := normalizeNetwork(network)
	if err != nil {
		return nil, err
	}
	if err := validateSCTPAddr(laddr, false); err != nil {
		return nil, err
	}
	laddr = cloneSCTPAddr(laddr)
	af, ipv6only := favoriteAddrFamily(network, laddr, nil, "listen")
	sock, err := syscall.Socket(
		af,
		syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC,
		syscall.IPPROTO_SCTP,
	)
	if err != nil {
		return nil, err
	}
	file, err := socketFile(sock)
	if err != nil {
		syscall.Close(sock)
		return nil, err
	}
	io := newSocketIO(file)

	// close socket on error
	defer func() {
		if err != nil {
			io.Close()
		}
	}()
	if err = setDefaultSockopts(sock, af, ipv6only); err != nil {
		return nil, err
	}
	if control != nil {
		rc := io
		var localAddressString string
		if laddr != nil {
			localAddressString = laddr.String()
		}
		if err = control(network, localAddressString, rc); err != nil {
			return nil, err
		}
	}
	err = setInitOpts(sock, options)
	if err != nil {
		return nil, err
	}

	if laddr != nil {
		// If IP address and/or port was not provided so far, let's use the unspecified IPv4 or IPv6 address
		if len(laddr.IPAddrs) == 0 {
			if af == syscall.AF_INET {
				laddr.IPAddrs = append(laddr.IPAddrs, net.IPAddr{IP: net.IPv4zero})
			} else if af == syscall.AF_INET6 {
				laddr.IPAddrs = append(laddr.IPAddrs, net.IPAddr{IP: net.IPv6zero})
			}
		}
		err = SCTPBind(sock, laddr, SCTP_BINDX_ADD_ADDR)
		if err != nil {
			return nil, err
		}
	}
	err = syscall.Listen(sock, syscall.SOMAXCONN)
	if err != nil {
		return nil, err
	}
	return &SCTPListener{
			fd:                  sock,
			file:                file,
			io:                  io,
			notificationHandler: notificationHandler,
		},
		nil
}

// FileListener takes a file, dup's the underlying file descriptor, and returns
// a SCTPListener created from the dup'd fd.
func FileListener(file *os.File) (*SCTPListener, error) {
	r1, _, err := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
	runtime.KeepAlive(file)
	if err != 0 {
		return nil, os.NewSyscallError("fcntl", err)
	}

	owned, initErr := socketFile(int(r1))
	if initErr != nil {
		syscall.Close(int(r1))
		return nil, initErr
	}

	return &SCTPListener{
		fd:                  int(r1),
		file:                owned,
		io:                  newSocketIO(owned),
		notificationHandler: nil,
	}, nil
}

// AcceptSCTP waits for and returns the next SCTP connection to the listener.
func (ln *SCTPListener) AcceptSCTP() (*SCTPConn, error) {
	raw, err := ln.SyscallConn()
	if err != nil {
		return nil, err
	}
	var fd int
	var acceptErr error
	err = raw.Read(func(listenFD uintptr) bool {
		for {
			fd, _, acceptErr = syscall.Accept4(int(listenFD), syscall.SOCK_CLOEXEC|syscall.SOCK_NONBLOCK)
			if acceptErr != syscall.EINTR {
				return acceptErr != syscall.EAGAIN
			}
		}
	})
	if err != nil {
		return nil, err
	}
	if acceptErr != nil {
		return nil, acceptErr
	}
	conn := NewSCTPConn(fd, ln.notificationHandler)
	conn.singleAssociation = true
	if conn.initErr != nil {
		conn.Close()
		return nil, conn.initErr
	}
	return conn, nil
}

// Accept waits for and returns the next connection connection to the listener.
func (ln *SCTPListener) Accept() (net.Conn, error) {
	conn, err := ln.AcceptSCTP()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (ln *SCTPListener) Close() error {
	if ln == nil || ln.file == nil {
		return syscall.EINVAL
	}
	return ln.io.Close()
}

func (ln *SCTPListener) SyscallConn() (syscall.RawConn, error) {
	if ln == nil || ln.file == nil {
		return nil, syscall.EINVAL
	}
	return ln.io, nil
}

// DialSCTP - bind socket to laddr (if given) and connect to raddr
func DialSCTP(net string, laddr, raddr *SCTPAddr) (*SCTPConn, error) {
	return DialSCTPExt(net, laddr, raddr, InitMsg{NumOstreams: SCTP_MAX_STREAM})
}

// DialSCTPExt - same as DialSCTP but with given SCTP options
func DialSCTPExt(network string, laddr, raddr *SCTPAddr, options InitMsg) (*SCTPConn, error) {
	return dialSCTPExtConfig(network, laddr, raddr, options, nil, nil)
}

// dialSCTPExtConfig - same as DialSCTP but with given SCTP options and socket configuration
func dialSCTPExtConfig(network string, laddr, raddr *SCTPAddr, options InitMsg, control func(network, address string, c syscall.RawConn) error, notificationHandler NotificationHandler) (*SCTPConn, error) {
	network, err := normalizeNetwork(network)
	if err != nil {
		return nil, err
	}
	if err := validateSCTPAddr(laddr, false); err != nil {
		return nil, err
	}
	laddr = cloneSCTPAddr(laddr)
	if err := validateSCTPAddr(raddr, true); err != nil {
		return nil, err
	}
	raddr = cloneSCTPAddr(raddr)
	af, ipv6only := favoriteAddrFamily(network, laddr, raddr, "dial")
	sock, err := syscall.Socket(
		af,
		syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC,
		syscall.IPPROTO_SCTP,
	)
	if err != nil {
		return nil, err
	}

	file, err := socketFile(sock)
	if err != nil {
		syscall.Close(sock)
		return nil, err
	}
	io := newSocketIO(file)

	// close socket on error
	defer func() {
		if err != nil {
			io.Close()
		}
	}()
	if err = setDefaultSockopts(sock, af, ipv6only); err != nil {
		return nil, err
	}
	if control != nil {
		rc := io
		var localAddressString string
		if laddr != nil {
			localAddressString = laddr.String()
		}
		if err = control(network, localAddressString, rc); err != nil {
			return nil, err
		}
	}
	err = setInitOpts(sock, options)
	if err != nil {
		return nil, err
	}
	if laddr != nil {
		// If IP address and/or port was not provided so far, let's use the unspecified IPv4 or IPv6 address
		if len(laddr.IPAddrs) == 0 {
			if af == syscall.AF_INET {
				laddr.IPAddrs = append(laddr.IPAddrs, net.IPAddr{IP: net.IPv4zero})
			} else if af == syscall.AF_INET6 {
				laddr.IPAddrs = append(laddr.IPAddrs, net.IPAddr{IP: net.IPv6zero})
			}
		}
		err = SCTPBind(sock, laddr, SCTP_BINDX_ADD_ADDR)
		if err != nil {
			return nil, err
		}
	}
	_, err = SCTPConnect(sock, raddr)
	if err == syscall.EINPROGRESS || err == syscall.EINTR || err == syscall.EALREADY || err == syscall.EISCONN {
		// A signal can restart CONNECTX3 after it has already started the
		// association. This is a fresh socket: wait for that association
		// instead of discarding it as a failed dial.
		err = waitForSCTPConnect(sock)
	}
	if err != nil {
		return nil, err
	}
	conn := &SCTPConn{file: file, io: io, notificationHandler: notificationHandler, singleAssociation: true}
	conn._fd.Store(int32(sock))
	return conn, nil
}

func peelOffSocket(fd, id int) (int, error) {
	param := struct {
		assocID int32
		sd      int32
		flags   uint32
	}{assocID: int32(id), flags: syscall.SOCK_CLOEXEC | syscall.SOCK_NONBLOCK}
	optlen := uint32(unsafe.Sizeof(param))
	_, _, err := getsockopt(fd, SCTP_SOCKOPT_PEELOFF_FLAGS, uintptr(unsafe.Pointer(&param)), &optlen)
	if err == syscall.ENOPROTOOPT {
		return peelOffSocketLegacy(fd, id)
	}
	return int(param.sd), err
}

func peelOffSocketLegacy(fd, id int) (int, error) {
	param := struct {
		assocID int32
		sd      int32
	}{assocID: int32(id)}
	optlen := uint32(unsafe.Sizeof(param))
	// Old kernels cannot atomically set CLOEXEC on a peeled socket. Exclude
	// Go's fork/exec until the descriptor has its close-on-exec flag.
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	_, _, err := getsockopt(fd, SCTP_SOCKOPT_PEELOFF, uintptr(unsafe.Pointer(&param)), &optlen)
	if err != nil {
		return -1, err
	}
	_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(param.sd), syscall.F_SETFD, syscall.FD_CLOEXEC)
	if errno != 0 {
		syscall.Close(int(param.sd))
		return -1, errno
	}
	return int(param.sd), nil
}

func waitForSCTPConnect(fd int) error {
	timeout, err := socketTimeout(fd, syscall.SO_SNDTIMEO)
	if err != nil {
		return err
	}
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	// ppoll avoids FD_SETSIZE limits and restarts only the readiness wait.
	pollfd := struct {
		fd              int32
		events, revents int16
	}{fd: int32(fd), events: 0x0004 /* POLLOUT */}
	for {
		var remaining *syscall.Timespec
		if !deadline.IsZero() {
			left := time.Until(deadline)
			if left <= 0 {
				return syscall.EINPROGRESS
			}
			ts := syscall.NsecToTimespec(left.Nanoseconds())
			remaining = &ts
		}
		n, _, errno := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&pollfd)), 1, uintptr(unsafe.Pointer(remaining)), 0, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		if n == 0 {
			return syscall.EINPROGRESS
		}
		break
	}
	code, err := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
	if err != nil {
		return err
	}
	if code != 0 {
		return syscall.Errno(code)
	}
	// Writable alone does not prove the connection succeeded.
	_, err = syscall.Getpeername(fd)
	return err
}

// associationReadError is used only after recvmsg EAGAIN on a known exclusive
// association. An endpoint's assoc_id=0 EINVAL is not a termination signal.
func associationReadError(fd int) error {
	for {
		var status Status
		size := uint32(unsafe.Sizeof(status))
		_, _, err := getsockopt(fd, SCTP_STATUS, uintptr(unsafe.Pointer(&status)), &size)
		if err == syscall.EINTR {
			continue
		}
		if err == syscall.EINVAL {
			return syscall.ENOTCONN
		}
		return err
	}
}
