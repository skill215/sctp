//go:build !linux || (linux && 386)

package sctp

func ParseNotification([]byte) (Notification, error) { return Notification{}, ErrUnsupported }
