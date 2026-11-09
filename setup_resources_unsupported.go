//go:build !linux || (linux && 386)

package sctp

func SetupRequirements(SetupConfig) (SetupResourceRequirements, error) {
	return SetupResourceRequirements{}, ErrUnsupported
}
