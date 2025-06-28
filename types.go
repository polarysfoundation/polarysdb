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
	"time"

	"github.com/polarysfoundation/polarys_db/modules/common"
	"github.com/polarysfoundation/polarys_db/modules/config"
)

// Database represents a simple in-memory database structure.
// It contains the following fields:
// - dbPath: a string representing the path to the database.
// - data: a nested map structure to store the database records.
// - mutex: a sync.Mutex to ensure thread-safe access to the database.
// - key: a common.Key used for database operations.
// - lastLoaded: a time.Time indicating the last time the database was loaded.
// - stopWatch: a channel used to signal when the database should stop watching for changes.
type Database struct {
	dbPath     string
	data       map[string]map[string]any
	mutex      sync.RWMutex
	key        common.Key
	lastLoaded time.Time
	stopWatch  chan struct{}
}

// Init initializes the database with the given encryption key and directory path.
// It creates the necessary directories if they do not exist and loads the database from the file.
func Init(keyDb common.Key, dirPath string) (*Database, error) {
	path := config.GetStateDBPath(dirPath)

	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	db := &Database{
		dbPath:    path,
		data:      make(map[string]map[string]any),
		key:       keyDb,
		stopWatch: make(chan struct{}),
	}

	// Cargar por primera vez
	if err := db.load(); err != nil {
		return nil, err
	}

	// Iniciar el watcher de cambios externos
	go db.fileOnChange()

	return db, nil
}

// Exist checks if a table exists in the database.
func (db *Database) Exist(table string) bool {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	if _, ok := db.data[table]; ok {
		return true
	}
	return false
}

// Create creates a new table in the database.
func (db *Database) Create(table string) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if _, ok := db.data[table]; !ok {
		db.data[table] = make(map[string]any)
	}

	return db.save()
}

// Write updates an existing record in the specified table with the given key and value.
// Returns an error if the table does not exist.
func (db *Database) Write(table, key string, value any) error {
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
func (db *Database) Read(table, key string) (any, bool) {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	if t, ok := db.data[table]; ok {
		value, exists := t[key]
		return value, exists
	}
	return nil, false
}

// ReadBatch retrieves all records from the specified table.
// Returns a slice of values.
func (db *Database) ReadBatch(table string) ([]any, error) {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	v := make([]any, 0)

	t, ok := db.data[table]
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", table)
	}

	for _, d := range t {
		v = append(v, d)
	}

	return v, nil
}

// fileOnChange monitors the database file for external changes and reloads it if a change is detected.
func (db *Database) fileOnChange() {
	// Create a new ticker that ticks every 3 seconds.
	ticker := time.NewTicker(3 * time.Second)
	// Ensure the ticker is stopped when the function exits.
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get file information for the database path.
			info, err := os.Stat(db.dbPath)
			// If there's an error getting file info or the modification time hasn't changed, continue to the next tick.
			if err != nil || info.ModTime().Equal(db.lastLoaded) {
				continue
			}

			// The file has changed, reload it.
			// Acquire a write lock to prevent concurrent access during reload.
			db.mutex.Lock()
			err = db.load()
			// Release the lock after loading.
			db.mutex.Unlock()

			if err != nil {
				fmt.Println("⚠️ Error updating database:", err)
			} else {
				fmt.Println("📥 Database updated successfully.")
			}
		// If the stopWatch channel receives a signal, exit the goroutine.
		case <-db.stopWatch:
			return
		}
	}
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
	info, err := os.Stat(db.dbPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	encryptedData, err := os.ReadFile(db.dbPath)
	if err != nil {
		return err
	}

	decryptedData, err := decrypt(encryptedData, db.key)
	if err != nil {
		return err
	}

	var newData map[string]map[string]any
	if err := json.Unmarshal(decryptedData, &newData); err != nil {
		return err
	}

	db.data = newData
	db.lastLoaded = info.ModTime()
	return nil
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

// Close stops the file change watcher goroutine.
func (db *Database) Close() {
	if db.stopWatch != nil {
		close(db.stopWatch)
	}
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
