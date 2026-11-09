//go:build !linux || (linux && 386)

package sctp

import "context"

func dialSCTPContext(context.Context, string, *SCTPAddr, *SCTPAddr, SocketConfig) (*SCTPConn, error) {
	return nil, ErrUnsupported
}
