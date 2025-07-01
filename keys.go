package polarysdb

import (
	"crypto/rand"
	"fmt"
	"io"

	pm256 "github.com/polarysfoundation/pm-256"
	"github.com/polarysfoundation/polarysdb/modules/common"
)

func GenerateKey() common.Key {
	b := make([]byte, 64)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		fmt.Println("Error generating random bytes:", err)
		return [32]byte{} // Return an empty array in case of error
	}

	h := pm256.Sum256(b)
	return common.BytesToKey(h[:])
}

func GenerateKeyFromBytes(b []byte) common.Key {
	h := pm256.Sum256(b)

	return common.BytesToKey(h[:])
}