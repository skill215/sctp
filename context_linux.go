//go:build linux && !386

package sctp

import (
	"context"
	"net"
	"syscall"
	"time"
)

func dialSCTPContext(ctx context.Context, network string, laddr, raddr *SCTPAddr, cfg SocketConfig) (_ *SCTPConn, err error) {
	af, ipv6only := favoriteAddrFamily(network, laddr, raddr, "dial")
	if err = contextError(ctx); err != nil {
		return nil, err
	}
	fd, err := syscall.Socket(af, syscall.SOCK_STREAM|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, syscall.IPPROTO_SCTP)
	if err != nil {
		return nil, err
	}
	file, err := socketFile(fd)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	owner := newSocketIO(file)
	published := false
	defer func() {
		if !published {
			owner.Close()
		}
	}()
	if err = contextError(ctx); err != nil {
		return nil, err
	}
	if err = setDefaultSockopts(fd, af, ipv6only); err != nil {
		return nil, err
	}
	// Unlike the legacy helper's ignored IPV6_V6ONLY result, the new entrypoint
	// checks all required setup. No user callback runs on this private descriptor.
	if af == syscall.AF_INET6 {
		if err = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, boolint(ipv6only)); err != nil {
			return nil, err
		}
	}
	if err = contextError(ctx); err != nil {
		return nil, err
	}
	if err = setInitOpts(fd, cfg.InitMsg); err != nil {
		return nil, err
	}
	if cfg.Messages != nil {
		if err = configureMessageFD(fd, *cfg.Messages); err != nil {
			return nil, err
		}
	}
	if laddr != nil {
		if len(laddr.IPAddrs) == 0 {
			ip := net.IPv4zero
			if af == syscall.AF_INET6 {
				ip = net.IPv6zero
			}
			laddr.IPAddrs = []net.IPAddr{{IP: ip}}
		}
		if err = contextError(ctx); err != nil {
			return nil, err
		}
		if err = SCTPBind(fd, laddr, SCTP_BINDX_ADD_ADDR); err != nil {
			return nil, err
		}
	}
	if err = contextError(ctx); err != nil {
		return nil, err
	}
	_, err = SCTPConnect(fd, raddr)
	if err != nil && err != syscall.EINPROGRESS && err != syscall.EINTR && err != syscall.EALREADY && err != syscall.EISCONN {
		return nil, err
	}
	// Do not repeat CONNECTX on EINTR; readiness must prove the original association.
	if err = waitContextConnect(ctx, owner); err != nil {
		return nil, err
	}
	conn := &SCTPConn{file: file, io: owner, notificationHandler: cfg.NotificationHandler, singleAssociation: true}
	conn._fd.Store(int32(fd))
	if err = finishMessageConn(conn, cfg.Messages); err != nil {
		return nil, err
	}
	if err = publishContextDial(ctx, conn); err != nil {
		return nil, err
	}
	published = true
	return conn, nil
}

func waitContextConnect(ctx context.Context, owner *socketIO) error {
	var result error
	err := owner.pollContext(ctx, false, time.Time{}, func(fd uintptr) bool {
		code, e := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_ERROR)
		if e == syscall.EINTR {
			return false
		}
		if e != nil {
			result = e
			return true
		}
		if code != 0 {
			result = syscall.Errno(code)
			return true
		}
		_, e = syscall.Getpeername(int(fd))
		if e == syscall.ENOTCONN || e == syscall.EINTR {
			return false
		}
		result = e
		return true
	})
	if err != nil {
		return err
	}
	return result
}

// Final publication point, after the poller's temporary deadline/hook retired.
// Failure cleanup remains owned by the caller until this function succeeds.
func publishContextDial(ctx context.Context, conn *SCTPConn) error {
	if e := conn.io.Control(func(uintptr) {}); e != nil {
		return e
	}
	return contextError(ctx)
}
