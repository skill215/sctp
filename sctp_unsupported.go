//go:build !linux || (linux && 386)

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
	"errors"
	"net"
	"os"
	"syscall"
)

func newSCTPConn(fd int, handler NotificationHandler) *SCTPConn {
	conn := &SCTPConn{notificationHandler: handler, initErr: ErrUnsupported}
	conn._fd.Store(-1)
	if fd >= 0 {
		if err := closeUnsupportedSocket(fd); err != nil {
			conn.initErr = errors.Join(ErrUnsupported, os.NewSyscallError("close socket", err))
		}
	}
	return conn
}

func (c *SCTPConn) SyscallConn() (syscall.RawConn, error) {
	return nil, c.unsupportedError()
}

func setsockopt(fd int, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	return 0, 0, ErrUnsupported
}

func getsockopt(fd int, optname, optval uintptr, optlen *uint32) (uintptr, uintptr, error) {
	return 0, 0, ErrUnsupported
}

func getSocketOption(fd int, level, optname, optval, optlen uintptr) (uintptr, uintptr, error) {
	return 0, 0, ErrUnsupported
}

func peelOffSocket(fd, id int) (int, error) {
	return -1, ErrUnsupported
}

func (c *SCTPConn) SCTPWrite(b []byte, info *SndRcvInfo) (int, error) {
	return 0, c.unsupportedError()
}

func (c *SCTPConn) SCTPRead(b []byte) (int, *SndRcvInfo, error) {
	return 0, nil, c.unsupportedError()
}

func (c *SCTPConn) Close() error {
	return c.unsupportedError()
}

func (c *SCTPConn) SetWriteBuffer(bytes int) error {
	return c.unsupportedError()
}

func (c *SCTPConn) GetWriteBuffer() (int, error) {
	return 0, c.unsupportedError()
}

func (c *SCTPConn) SetReadBuffer(bytes int) error {
	return c.unsupportedError()
}

func (c *SCTPConn) GetReadBuffer() (int, error) {
	return 0, c.unsupportedError()
}

func ListenSCTP(net string, laddr *SCTPAddr) (*SCTPListener, error) {
	return nil, ErrUnsupported
}

func ListenSCTPExt(net string, laddr *SCTPAddr, options InitMsg) (*SCTPListener, error) {
	return nil, ErrUnsupported
}

func listenSCTPExtConfig(network string, laddr *SCTPAddr, options InitMsg, control func(network string, address string, c syscall.RawConn) error, handler NotificationHandler) (*SCTPListener, error) {
	return nil, ErrUnsupported
}

func FileListener(file *os.File) (*SCTPListener, error) {
	return nil, ErrUnsupported
}

func (ln *SCTPListener) Accept() (net.Conn, error) {
	return nil, ErrUnsupported
}

func (ln *SCTPListener) AcceptSCTP() (*SCTPConn, error) {
	return nil, ErrUnsupported
}

func (ln *SCTPListener) Close() error {
	return ErrUnsupported
}

func DialSCTP(net string, laddr, raddr *SCTPAddr) (*SCTPConn, error) {
	return nil, ErrUnsupported
}

func DialSCTPExt(network string, laddr, raddr *SCTPAddr, options InitMsg) (*SCTPConn, error) {
	return nil, ErrUnsupported
}

func dialSCTPExtConfig(network string, laddr, raddr *SCTPAddr, options InitMsg, control func(network string, address string, c syscall.RawConn) error, handler NotificationHandler) (*SCTPConn, error) {
	return nil, ErrUnsupported
}

func (c *SCTPConn) unsupportedError() error {
	if c != nil && c.initErr != nil {
		return c.initErr
	}
	return ErrUnsupported
}

func (ln *SCTPListener) SyscallConn() (syscall.RawConn, error) {
	return nil, ErrUnsupported
}
