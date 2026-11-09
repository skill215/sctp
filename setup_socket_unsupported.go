//go:build !linux || (linux && 386)

package sctp

import (
	"context"
	"time"
)

func (*SCTPListener) AcceptSCTPContext(context.Context) (*SCTPConn, error) {
	return nil, ErrUnsupported
}
func (*SCTPListener) SetDeadline(time.Time) error                      { return ErrUnsupported }
func (*SCTPListener) BoundAddr() (*SCTPAddr, error)                    { return nil, ErrUnsupported }
func (*SCTPConn) SetSocketTimeouts(time.Duration, time.Duration) error { return ErrUnsupported }

func listenSetupContext(context.Context, string, *SCTPAddr, SocketConfig) (*SCTPListener, error) {
	return nil, ErrUnsupported
}
