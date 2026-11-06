package sctp

import "syscall"

// Linux sctp_assoc_t is signed 32-bit. Public int parameters must not select
// another association by losing their high bits at the syscall boundary.
func validateAssociationID(id int) error {
	if id != int(int32(id)) {
		return syscall.EINVAL
	}
	return nil
}
