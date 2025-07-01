package common

import "encoding/hex"

func IsEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func has0xPrefix(s string) bool {
	return len(s) >= 2 && s[0] == '0' && s[1] == 'x'
}

func encode(dst []byte, src []byte) []byte {
	hex.Encode(dst, src)
	return dst
}

func decode(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}
