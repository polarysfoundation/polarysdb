package polarysdb

import (
	"bytes"
	"log"
	"testing"

	pm256 "github.com/polarysfoundation/pm-256"
	"github.com/polarysfoundation/polarysdb/modules/common"
)

func TestGenerateKey(t *testing.T) {
	log.Println("Starting TestGenerateKey")
	key := GenerateKey()
	log.Printf("Generated key: %x", key)

	if len(key) != 32 {
		t.Errorf("Expected key length of 32, but got %d", len(key))
	}

	// Ensure the key is not all zeros
	if bytes.Equal(key[:], make([]byte, 32)) {
		t.Error("Generated key is all zeros, which is unexpected")
	}
	log.Println("Finished TestGenerateKey")
}

func TestGenerateKeyFromBytes(t *testing.T) {
	log.Println("Starting TestGenerateKeyFromBytes")
	input := []byte("test input")
	log.Printf("Input: %s", input)

	h := pm256.Sum256(input)
	expectedKey := common.BytesToKey(h[:])
	log.Printf("Expected key: %x", expectedKey)

	key := GenerateKeyFromBytes(input)
	log.Printf("Generated key from bytes: %x", key)

	if !bytes.Equal(key[:], expectedKey[:]) {
		t.Errorf("Expected key %x, but got %x", expectedKey, key)
	}
	log.Println("Finished TestGenerateKeyFromBytes")
}
