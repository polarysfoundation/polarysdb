package common

const keyLen = 32

type Key [keyLen]byte

func BytesToKey(b []byte) Key {
	var a Key
	a.SetBytes(b)
	return a
}

func StringToKey(s string) Key {
	return BytesToKey([]byte(s))
}

func (k Key) KeyToByte() []byte {
	return k[:]
}

func (a *Key) SetBytes(b []byte) {
	if len(b) > len(a) {
		b = b[len(b)-keyLen:]
	}

	copy(a[keyLen-len(b):], b)
}
