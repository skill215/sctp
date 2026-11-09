//go:build !linux || (linux && 386)

package sctp

import "context"

func MessageIORequirements(MessageOptions) (IORequirements, error) {
	return IORequirements{}, ErrUnsupported
}
func (*SCTPConn) GetMessageOptions() (MessageOptionState, error) {
	return MessageOptionState{}, ErrUnsupported
}
func (*SCTPConn) ReadEvent(context.Context, []byte, []byte, []byte) (ReadResult, error) {
	return ReadResult{}, ErrUnsupported
}
func (*SCTPConn) WriteMsgContext(context.Context, []byte, SndInfo) (int, error) {
	return 0, ErrUnsupported
}
