package sctp

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestNativeCodecLayout(t *testing.T) {
	info := SndRcvInfo{
		Stream: 0x1122, SSN: 0x3344, Flags: 0x5566,
		PPID: 0x778899aa, Context: 0xbbccddee, TTL: 0x01020304,
		TSN: 0x05060708, CumTSN: 0x090a0b0c, AssocID: 0x10203040,
	}
	want := "2211443366550000aa998877eeddccbb04030201080706050c0b0a0940302010"
	if !nativeLittleEndian {
		want = "1122334455660000778899aabbccddee0102030405060708090a0b0c10203040"
	}
	encoded := toBuf(&info)
	if got := hex.EncodeToString(encoded); got != want {
		t.Fatalf("native SCTP layout=%s, want %s", got, want)
	}
	// Byte slices may start at any offset; decoding must not require alignment.
	unaligned := append([]byte{0}, encoded...)[1:]
	before := append([]byte(nil), unaligned...)
	var decoded SndRcvInfo
	if err := decodeNative(unaligned, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != info || !bytes.Equal(unaligned, before) {
		t.Fatalf("decode changed the value or input: %+v", decoded)
	}
}

func TestNativeCodecTruncated(t *testing.T) {
	info := SndRcvInfo{Stream: 7, PPID: 0x01020304}
	encoded := toBuf(&info)
	for size := range encoded {
		decoded := SndRcvInfo{PPID: 123}
		before := decoded
		if err := decodeNative(encoded[:size], &decoded); err == nil {
			t.Fatalf("accepted %d-byte SCTP header", size)
		}
		if decoded != before {
			t.Fatalf("truncated decode changed output at %d bytes", size)
		}
	}
}
