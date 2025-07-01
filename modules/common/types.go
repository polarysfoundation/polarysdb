package common

// keyLen defines the length of the Key in bytes.
const keyLen = 32

// Key represents a fixed-size array of bytes.
type Key [keyLen]byte

// BytesToKey converts a byte slice to a Key.
func BytesToKey(b []byte) Key {
	var a Key
	a.SetBytes(b)
	return a
}

// StringToKey converts a string to a Key.
func StringToKey(s string) Key {
	return BytesToKey([]byte(s))
}

// String converts a Key to a string.
func (k Key) String() string {
	return string(k.hex())
}

// Hex converts a Key to a hexadecimal string.
func (k Key) Hex() string {
	return string(k.hex())
}

// KeyToByte converts a Key to a byte slice.
func (k Key) Bytes() []byte {
	return k[:]
}

// HexToKey converts a hexadecimal string to a Key.
func HexToKey(s string) Key {
	if len(s) > 2 && s[:2] == "0x" {
		s = s[2:]
	}
	return StringToKey(s)
}

// SetBytes sets the Key to the value of the given byte slice.
// If the byte slice is longer than the Key, only the last keyLen bytes are used.
func (a *Key) SetBytes(b []byte) {
	if len(b) > len(a) {
		b = b[len(b)-keyLen:]
	}

	copy(a[keyLen-len(b):], b)
}

// hex returns the hexadecimal representation of the Key, prefixed with "0x".
func (k Key) hex() []byte {
	buf := make([]byte, len(k)*2+2)
	copy(buf[:2], []byte("0x"))
	encode(buf[2:], k[:])
	return buf
}
