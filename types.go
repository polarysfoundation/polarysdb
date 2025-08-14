package polarysdb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/common"
	"github.com/polarysfoundation/polarysdb/modules/config"
	"github.com/polarysfoundation/polarysdb/modules/crypto"
	"github.com/polarysfoundation/polarysdb/modules/logger"
)

// Database represents a simple in-memory database structure.
// It contains the following fields:
// - dbPath: a string representing the path to the database.
// - data: a nested map structure to store the database records.
// - mutex: a sync.RWMutex to ensure thread-safe access to the database.
// - key: a common.Key used for database operations.
// - lastLoaded: a time.Time indicating the last time the database was loaded.
// - stopWatch: a channel used to signal when the database should stop watching for changes.
type Database struct {
	dbPath     string
	data       map[string]map[string]any
	mutex      sync.RWMutex
	key        common.Key
	lastLoaded time.Time
	logger     *logger.Logger
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
	// Initialize logger
	logCfg := logger.Config{
		MinLevel:  logger.LevelInfo,
		ToConsole: true,
		ToFile:    false,
	}
	l := logger.NewLogger(logCfg)

	db := &Database{
		dbPath:    path,
		data:      make(map[string]map[string]any),
		key:       keyDb,
		stopWatch: make(chan struct{}),
		logger:    l,
	}

	// Load for the first time
	if err := db.load(); err != nil {
		return nil, err
	}

	// Start the external change watcher
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

// Export saves the current in-memory database to a plain, unencrypted JSON file.
func (db *Database) Export(key common.Key, path string) error {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to export database")
	}

	data, err := json.MarshalIndent(db.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// Import loads data from a plain, unencrypted JSON file into the in-memory database,
// replacing its current content, and then saves the encrypted database to disk.
func (db *Database) Import(key common.Key, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to export database")
	}

	var importedData map[string]map[string]any
	if err := json.Unmarshal(data, &importedData); err != nil {
		return err
	}

	for table, records := range importedData {
		if table == "" {
			return fmt.Errorf("invalid table name in import file")
		}
		if records == nil {
			return fmt.Errorf("invalid record data for table %s", table)
		}
	}

	db.mutex.Lock()
	db.data = importedData
	db.mutex.Unlock()

	return db.save()
}

// ExportEncrypted saves the current in-memory database to an encrypted JSON file.
func (db *Database) ExportEncrypted(key common.Key, path string) error {
	db.mutex.RLock()
	defer db.mutex.RUnlock()

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to export database")
	}

	data, err := json.MarshalIndent(db.data, "", "  ")
	if err != nil {
		return err
	}

	encryptedData, err := crypto.Encrypt(data, db.key)
	if err != nil {
		return err
	}

	return os.WriteFile(path, encryptedData, 0600)
}

// ImportEncrypted loads data from an encrypted file into the in-memory database,
// replacing its current content, and then saves the encrypted database to disk.
func (db *Database) ImportEncrypted(key common.Key, path string) error {
	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to export database")
	}

	decryptedData, err := crypto.Decrypt(encryptedData, db.key)
	if err != nil {
		return err
	}

	var importedData map[string]map[string]any
	if err := json.Unmarshal(decryptedData, &importedData); err != nil {
		return err
	}

	for table, records := range importedData {
		if table == "" {
			return fmt.Errorf("invalid table name in import file")
		}
		if records == nil {
			return fmt.Errorf("invalid record data for table %s", table)
		}
	}

	db.mutex.Lock()
	db.data = importedData
	db.mutex.Unlock()

	return db.save()
}

// ChangeKey changes the encryption key of the database.
func (db *Database) ChangeKey(oldKey, newKey common.Key) error {
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if !common.IsEqual(db.key[:], oldKey[:]) {
		return fmt.Errorf("old key does not match current database key")
	}

	db.key = newKey
	return db.save()
}

// fileOnChange monitors the database file for external changes and reloads it if a change is detected.
func (db *Database) fileOnChange() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-db.stopWatch:
			return
		case <-ticker.C:
			info, err := os.Stat(db.dbPath)
			if err != nil {
				if !os.IsNotExist(err) {
					db.logger.Error("error stating database file for changes: ", err)
				}
				continue
			}

			if info.ModTime().Equal(db.lastLoaded) && !db.lastLoaded.IsZero() {
				continue
			}

			db.mutex.Lock()
			err = db.load()
			db.mutex.Unlock()

			if err != nil {
				db.logger.Warn("Error reloading database from file: ", err)
			} else {
				db.logger.Info("Database reloaded from file successfully.")
			}
		}
	}
}

// save serializes the database data to JSON, encrypts it, and writes it to the file.
func (db *Database) save() error {
	data, err := json.Marshal(db.data)
	if err != nil {
		return err
	}

	encryptedData, err := crypto.Encrypt(data, db.key)
	if err != nil {
		return err
	}

	return os.WriteFile(db.dbPath, encryptedData, 0600)
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

	decryptedData, err := crypto.Decrypt(encryptedData, db.key)
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

// Close stops the file change watcher goroutine.
func (db *Database) Close() {
	if db.stopWatch != nil {
		close(db.stopWatch)
		db.stopWatch = nil
	}
}
