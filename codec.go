package sctp

import "encoding/binary"

func toBuf(v any) []byte {
	buf, _ := binary.Append(nil, nativeEndian, v)
	return buf
}

func decodeNative(buf []byte, v any) error {
	_, err := binary.Decode(buf, nativeEndian, v)
	return err
}
