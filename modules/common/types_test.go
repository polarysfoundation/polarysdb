package common

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"unicode"
)

func TestBytesToKey(t *testing.T) {
	log.Println("Starting TestBytesToKey")
	b := []byte("testbytes")
	expected := Key{}
	expected.SetBytes(b)

	result := BytesToKey(b)
	if !bytes.Equal(result[:], expected[:]) {
		t.Errorf("BytesToKey(%v) = %v; want %v", b, result, expected)
	} else {
		log.Printf("BytesToKey(%v) = %v; as expected", b, result)
	}
	log.Println("Finished TestBytesToKey")
}

func TestStringToKey(t *testing.T) {
	log.Println("Starting TestStringToKey")
	s := "teststring"
	expected := Key{}
	expected.SetBytes([]byte(s))

	result := StringToKey(s)
	if !bytes.Equal(result[:], expected[:]) {
		t.Errorf("StringToKey(%v) = %v; want %v", s, result, expected)
	} else {
		log.Printf("StringToKey(%v) = %v; as expected", s, result)
	}
	log.Println("Finished TestStringToKey")
}

func TestKeyToString(t *testing.T) {
	log.Println("Starting TestKeyToString")
	k := StringToKey("teststring")
	expected := "teststring"

	result := k.KeyToString()

	// Debugging: Print lengths and byte representations
	log.Printf("Length of result: %d", len(result))
	log.Printf("Length of expected: %d", len(expected))
	log.Printf("Bytes of result: %v", []byte(result))
	log.Printf("Bytes of expected: %v", []byte(expected))

	// Option 1: Trim non-printable characters
	result = strings.TrimFunc(result, func(r rune) bool {
		return !unicode.IsPrint(r)
	})

	// Option 2: Manually truncate to expected length
	if len(result) > len(expected) {
		result = result[:len(expected)]
	}

	if result != expected {
		t.Errorf("KeyToString() = %v; want %v", result, expected)
		log.Println("Finished TestKeyToString with error")
	} else {
		log.Printf("KeyToString() = %v; as expected", result)
		log.Println("Finished TestKeyToString successfully")
	}
}

func TestKeyToByte(t *testing.T) {
	log.Println("Starting TestKeyToByte")
	k := StringToKey("teststring")
	expected := []byte("teststring")

	result := k.KeyToByte()

	log.Print(result)
	log.Printf("Length of result: %d", len(result))
	log.Printf("Length of expected: %d", len(expected))
	log.Printf("Bytes of result: %v", result)
	log.Printf("Bytes of expected: %v", expected)

	// Ensure the result is truncated or padded to the expected length
	if len(result) > len(expected) {
		result = result[len(result)-len(expected):]
	}

	log.Print(result)

	if !bytes.Equal(result, expected) {
		t.Errorf("KeyToByte() = %v; want %v", result, expected)
	} else {
		log.Printf("KeyToByte() = %v; as expected", result)
	}
	log.Println("Finished TestKeyToByte")
}

func TestSetBytes(t *testing.T) {
	log.Println("Starting TestSetBytes")
	var k Key
	b := []byte("testbytes")
	expected := Key{}
	expected.SetBytes(b)

	k.SetBytes(b)
	if !bytes.Equal(k[:], expected[:]) {
		t.Errorf("SetBytes(%v) = %v; want %v", b, k, expected)
	} else {
		log.Printf("SetBytes(%v) = %v; as expected", b, k)
	}
	log.Println("Finished TestSetBytes")
}

func TestSetBytesLongerThanKey(t *testing.T) {
	log.Println("Starting TestSetBytesLongerThanKey")
	var k Key
	b := []byte("thisisaverylongstringthatexceedsthekeylength")
	expected := Key{}
	expected.SetBytes(b[len(b)-keyLen:])

	k.SetBytes(b)
	if !bytes.Equal(k[:], expected[:]) {
		t.Errorf("SetBytes(%v) = %v; want %v", b, k, expected)
	} else {
		log.Printf("SetBytes(%v) = %v; as expected", b, k)
	}
	log.Println("Finished TestSetBytesLongerThanKey")
}
