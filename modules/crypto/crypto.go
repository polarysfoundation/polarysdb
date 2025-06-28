package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/polarysfoundation/polarys_db/modules/common"
)

// encrypt encrypts the given data using AES encryption with the provided key.
func Encrypt(data []byte, key common.Key) ([]byte, error) {
	block, err := aes.NewCipher(key.KeyToByte())
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

// decrypt decrypts the given encrypted data using AES decryption with the provided key.
func Decrypt(encryptedData []byte, key common.Key) ([]byte, error) {
	block, err := aes.NewCipher(key.KeyToByte())
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(encryptedData) < nonceSize {
		return nil, fmt.Errorf("invalid encrypted data")
	}

	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
