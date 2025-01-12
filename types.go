package polarysdb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/polarysfoundation/polarys_db/modules/common"
	"github.com/polarysfoundation/polarys_db/modules/config"
)

// Database represents a simple in-memory database structure.
// It contains the following fields:
// - dbPath: a string representing the path to the database.
// - data: a nested map structure to store the database records.
// - mutex: a sync.Mutex to ensure thread-safe access to the database.
// - key: a common.Key used for database operations.
type Database struct {
	dbPath string
	data   map[string]map[string]interface{}
	mutex  sync.Mutex
	key    common.Key
}

// Init initializes the database with the given encryption key and directory path.
// It creates the necessary directories if they do not exist and loads the database from the file.
func Init(keyDb string, dirPath string) (*Database, error) {
	path := config.GetStateDBPath(dirPath)

	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return nil, err
		}
	}

	db := &Database{
		dbPath: path,
		data:   make(map[string]map[string]interface{}),
		key:    common.StringToKey(keyDb), // Usar la clave de cifrado proporcionada
	}

	if err := db.load(); err != nil {
		return nil, err
	}

	return db, nil
}

// Exist checks if a table exists in the database.
func (db *Database) Exist(table string) bool {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	_, ok := db.data[table]

	return ok
}

// Create creates a new record in the specified table with the given key and value.
// If the table does not exist, it is created.
func (db *Database) Create(table, key string, value interface{}) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if _, ok := db.data[table]; !ok {
		db.data[table] = make(map[string]interface{})
	}
	db.data[table][key] = value

	return db.save()
}

// Write updates an existing record in the specified table with the given key and value.
// Returns an error if the table does not exist.
func (db *Database) Write(table, key string, value interface{}) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if _, ok := db.data[table]; !ok {
		return fmt.Errorf("table %s does not exist", table)
	}
	db.data[table][key] = value
	return db.save()
}

// Delete removes a record from the specified table with the given key.
// Returns an error if the table does not exist.
func (db *Database) Delete(table, key string) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if t, ok := db.data[table]; ok {
		delete(t, key)
		return db.save()
	}
	return fmt.Errorf("table %s does not exist", table)
}

// Read retrieves a record from the specified table with the given key.
// Returns the value and a boolean indicating if the key exists.
func (db *Database) Read(table, key string) (interface{}, bool) {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if t, ok := db.data[table]; ok {
		value, exists := t[key]
		return value, exists
	}
	return nil, false
}

// ReadBatch retrieves all records from the specified table.
// Returns a slice of values.
func (db *Database) ReadBatch(table string) []interface{} {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	v := make([]interface{}, 0)

	t, ok := db.data[table]
	if !ok {
		return nil
	}

	for _, d := range t {
		v = append(v, d)
	}

	return v
}

// save serializes the database data to JSON, encrypts it, and writes it to the file.
func (db *Database) save() error {
	data, err := json.Marshal(db.data)
	if err != nil {
		return err
	}

	encryptedData, err := encrypt(data, db.key)
	if err != nil {
		return err
	}

	return os.WriteFile(db.dbPath, encryptedData, 0644)
}

// load reads the encrypted database file, decrypts it, and deserializes the JSON data into the database.
func (db *Database) load() error {
	if _, err := os.Stat(db.dbPath); os.IsNotExist(err) {
		return nil // Si el archivo no existe, es un caso válido
	}

	encryptedData, err := os.ReadFile(db.dbPath)
	if err != nil {
		return err
	}

	decryptedData, err := decrypt(encryptedData, db.key)
	if err != nil {
		return err
	}

	return json.Unmarshal(decryptedData, &db.data)
}

// encrypt encrypts the given data using AES encryption with the provided key.
func encrypt(data []byte, key common.Key) ([]byte, error) {
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
func decrypt(encryptedData []byte, key common.Key) ([]byte, error) {
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
