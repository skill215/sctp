//go:build linux && !386

package sctp_test

import (
	"encoding/binary"
	"fmt"
	sctp "github.com/skill215/sctp"
)

func ExampleParseNotification() {
	// In a real reader pass notificationBuffer[:result.N] after ReadEvent.
	p := make([]byte, 12)
	binary.NativeEndian.PutUint16(p, uint16(sctp.SCTP_SHUTDOWN_EVENT))
	binary.NativeEndian.PutUint32(p[4:], 12)
	binary.NativeEndian.PutUint32(p[8:], 7)
	n, err := sctp.ParseNotification(p)
	if err != nil {
		return
	}
	fmt.Println(n.Kind == sctp.NotificationShutdown, n.Shutdown.AssocID)
	// Output: true 7
}
