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

func (k Key) KeyToString() string {
	return string(k[:])
}

// KeyToByte converts a Key to a byte slice.
func (k Key) KeyToByte() []byte {
	return k[:]
}

// SetBytes sets the Key to the value of the given byte slice.
// If the byte slice is longer than the Key, only the last keyLen bytes are used.
func (a *Key) SetBytes(b []byte) {
	if len(b) > len(a) {
		b = b[len(b)-keyLen:]
	}

	copy(a[keyLen-len(b):], b)
}
