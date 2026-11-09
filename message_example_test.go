//go:build linux && !386

package sctp_test

import (
	"fmt"
	sctp "github.com/skill215/sctp"
)

func ExampleMessageIORequirements() {
	options := sctp.MessageOptions{ReceiveInfo: sctp.ReceiveInfoSNDRCV, Events: sctp.SCTP_EVENT_ASSOCIATION, AllowFragmentation: true}
	sizes, err := sctp.MessageIORequirements(options)
	if err != nil {
		return
	} // unsupported platforms do not fabricate a layout
	fmt.Println(sizes.NotificationMin, sizes.OOBMin >= 32)
	// Output: 20 true
}
