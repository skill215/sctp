//go:build linux && !386

package sctp

import "time"

func (c *SCTPConn) setPublicDeadline(read, write bool, t time.Time) error {
	raw, err := c.socketRaw()
	if err != nil {
		return err
	}
	return raw.setDeadlines(read, write, t)
}
