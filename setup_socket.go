package sctp

import "errors"

// ErrBoundAddrUnavailable means this listener was imported without a binding
// snapshot. BoundAddr never falls back to enumerating local interfaces.
var ErrBoundAddrUnavailable = errors.New("SCTP binding snapshot unavailable")

// SocketTimeoutUpdateError reports a failed SOL_SOCKET timeout update. The two
// setsockopt calls are not a transaction. ReadApplied means the read timeout was
// installed before the write timeout failed; callers taking over a socket should
// close it on any update failure. Err retains the native error identity.
type SocketTimeoutUpdateError struct {
	Option      string
	ReadApplied bool
	Err         error
}

func (e *SocketTimeoutUpdateError) Error() string {
	return "SCTP socket timeout update " + e.Option + ": " + e.Err.Error()
}
func (e *SocketTimeoutUpdateError) Unwrap() error { return e.Err }
