package polarysdb

import (
	"context"
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
type Database struct {
	dbPath     string
	data       map[string]map[string]any
	mutex      sync.RWMutex
	key        common.Key
	lastLoaded time.Time
	logger     *logger.Logger

	// Context and synchronization primitives for managing the file watcher goroutine.
	ctx        context.Context
	cancel     context.CancelFunc
	watcherWg  sync.WaitGroup
	closed     bool
	closeMutex sync.Mutex
}

// Init initializes the database with the given encryption key and directory path.
// It sets up the database structure, creates the necessary directories,
// initializes the logger, loads the initial data from the database file,
// and starts a goroutine to watch for external file changes.
// Returns a pointer to the initialized Database and an error if any occurs.
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

	// Create a context with cancellation for graceful shutdown of the watcher.
	ctx, cancel := context.WithCancel(context.Background())

	db := &Database{
		dbPath: path,
		data:   make(map[string]map[string]any),
		key:    keyDb,
		logger: l,
		ctx:    ctx,
		cancel: cancel,
		closed: false,
	}

	// Load the database content for the first time.
	if err := db.load(); err != nil {
		// Cancel the context if initial load fails to prevent the watcher from starting.
		cancel()
		return nil, err
	}

	// Start the external file change watcher in a new goroutine.
	// The WaitGroup ensures that the main program waits for the watcher
	// to finish before exiting during database closure.
	db.watcherWg.Add(1)
	go db.fileOnChange()

	return db, nil
}

// Exist checks if a table exists in the database.
// It acquires a read lock to safely access the database's internal map.
// Returns true if the table exists, false otherwise, or if the database is closed.
// This function is thread-safe.
func (db *Database) Exist(table string) bool {
	if db.isClosed() {
		return false
	}

	db.mutex.RLock()
	defer db.mutex.RUnlock()

	_, ok := db.data[table]
	return ok
}

// Create creates a new table in the database.
// It acquires a write lock to safely modify the database's internal map.
// If the table already exists, it does nothing.
// After creating the table, it saves the changes to the persistent storage.
// Returns an error if the database is closed or if saving fails. This function is thread-safe.
func (db *Database) Create(table string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()

	if _, ok := db.data[table]; !ok {
		db.data[table] = make(map[string]any)
	}

	return db.save()
}

// Write updates an existing record in the specified table with the given key and value.
// It acquires a write lock to safely modify the database's internal map.
// If the table does not exist, it returns an error.
// After updating the record, it saves the changes to the persistent storage.
// Returns an error if the database is closed, the table does not exist, or saving fails. This function is thread-safe.
func (db *Database) Write(table, key string, value any) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()

	if _, ok := db.data[table]; !ok {
		return fmt.Errorf("table %s does not exist", table)
	}
	db.data[table][key] = value
	return db.save()
}

// Delete removes a record from the specified table with the given key.
func (db *Database) Delete(table, key string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()

	if t, ok := db.data[table]; ok {
		delete(t, key)
		return db.save()
	}
	return fmt.Errorf("table %s does not exist", table)
}

// Read retrieves a record from the specified table with the given key.
func (db *Database) Read(table, key string) (any, bool) {
	if db.isClosed() {
		return nil, false
	}

	db.mutex.RLock()
	defer db.mutex.RUnlock()

	if t, ok := db.data[table]; ok {
		value, exists := t[key]
		return value, exists
	}
	return nil, false
}

// ReadBatch retrieves all records from the specified table.
func (db *Database) ReadBatch(table string) ([]any, error) {
	if db.isClosed() {
		return nil, fmt.Errorf("database is closed")
	}

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
// It requires the correct encryption key for authorization.
// The data is marshaled into a human-readable JSON format with indentation.
// Returns an error if the database is closed, the key is unauthorized,
// marshaling fails, or writing to the file fails. This function is thread-safe.
func (db *Database) Export(key common.Key, path string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

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

// Import loads data from a plain, unencrypted JSON file into the in-memory database.
// It requires the correct encryption key for authorization.
// The data is read from the specified file, unmarshaled from JSON,
// and replaces the current in-memory database content.
// Returns an error if the database is closed, the key is unauthorized,
// reading the file fails, unmarshaling fails, or the imported data is invalid. This function is thread-safe.
func (db *Database) Import(key common.Key, path string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to import database")
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
// It requires the correct encryption key for authorization.
// The data is marshaled into JSON, then encrypted using the database's internal key.
// Returns an error if the database is closed, the key is unauthorized,
// marshaling/encryption fails, or writing to the file fails. This function is thread-safe.
func (db *Database) ExportEncrypted(key common.Key, path string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

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

// ImportEncrypted loads data from an encrypted file into the in-memory database.
// It requires the correct encryption key for authorization.
// The data is read from the specified file, decrypted using the database's internal key,
// unmarshaled from JSON, and replaces the current in-memory database content.
// Returns an error if the database is closed, the key is unauthorized,
// reading/decryption fails, unmarshaling fails, or the imported data is invalid. This function is thread-safe.
func (db *Database) ImportEncrypted(key common.Key, path string) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if !common.IsEqual(key[:], db.key[:]) {
		return fmt.Errorf("unauthorized access to import database")
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
// It requires the old key for authorization.
// The database's internal key is updated, and the changes are saved to persistent storage
// using the new key.
// Returns an error if the database is closed, the old key does not match, or saving fails. This function is thread-safe.
func (db *Database) ChangeKey(oldKey, newKey common.Key) error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

	db.mutex.Lock()
	defer db.mutex.Unlock()

	if !common.IsEqual(db.key[:], oldKey[:]) {
		return fmt.Errorf("old key does not match current database key")
	}

	db.key = newKey
	return db.save()
}

// isClosed checks if the database is currently marked as closed.
// It uses a mutex to ensure thread-safe access to the `closed` flag.
// Returns true if the database is closed, false otherwise.
func (db *Database) isClosed() bool {
	db.closeMutex.Lock()
	defer db.closeMutex.Unlock()
	return db.closed
}

// fileOnChange monitors the database file for external changes with proper cancellation.
func (db *Database) fileOnChange() {
	defer db.watcherWg.Done()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	db.logger.Info("File watcher started")

	for {
		select {
		case <-db.ctx.Done():
			db.logger.Info("File watcher stopped")
			return
		case <-ticker.C:
			if db.isClosed() {
				db.logger.Info("Database closed, stopping file watcher")
				return
			}

			info, err := os.Stat(db.dbPath)
			if err != nil {
				if !os.IsNotExist(err) {
					db.logger.Error("error stating database file for changes: ", err)
				}
				continue
			}

			// If the file modification time hasn't changed since the last load, continue.
			if info.ModTime().Equal(db.lastLoaded) && !db.lastLoaded.IsZero() {
				continue
			}

			// Intentar cargar los cambios
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
// It assumes the caller has already acquired the necessary write lock.
// Returns an error if marshaling fails, encryption fails, or writing to the file fails.
// This is an internal helper function.
func (db *Database) save() error {
	if db.isClosed() {
		return fmt.Errorf("database is closed")
	}

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

// load reads the encrypted database file, decrypts it, and deserializes the JSON data.
// It assumes the caller has already acquired the necessary lock (read or write).
// If the file does not exist, it returns nil (indicating an empty database).
// Returns an error if reading the file fails, decryption fails, or unmarshaling fails. This is an internal helper function.
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

// Close stops the file change watcher goroutine gracefully and waits for it to finish.
// It marks the database as closed, cancels the context to signal the watcher,
// and waits for the watcher goroutine to complete with a default timeout of 10 seconds.
// Returns an error if the watcher does not stop within the timeout.
// This function is thread-safe and idempotent (calling it multiple times has no additional effect).
func (db *Database) Close() error {
	db.closeMutex.Lock()
	if db.closed {
		db.closeMutex.Unlock()
		return nil // Already closed
	}
	db.closed = true
	db.closeMutex.Unlock()
	
	// Cancel the context to signal the watcher goroutine to stop.
	if db.cancel != nil {
		db.cancel()
	}

	// Esperar con timeout a que termine el watcher
	done := make(chan struct{})
	go func() {
		db.watcherWg.Wait()
		close(done)
	}()
	
	// Wait for the watcher to finish or timeout.
	select {
	case <-done:
		db.logger.Info("Database closed successfully")
		return nil
	case <-time.After(10 * time.Second):
		// If the watcher doesn't stop within the timeout, log a warning
		// and return an error, but the database is still marked as closed
		// and the context is cancelled.
		db.logger.Warn("Timeout waiting for file watcher to stop")
		return fmt.Errorf("timeout waiting for file watcher to stop")
	}
}

// CloseWithTimeout stops the file change watcher goroutine gracefully,
// allowing a custom timeout duration for it to finish.
// It functions identically to `Close`, but provides flexibility in the waiting period
// for the file watcher to terminate.
// Returns an error if the watcher does not stop within the specified timeout.
// This function is thread-safe and idempotent.
func (db *Database) CloseWithTimeout(timeout time.Duration) error {
	db.closeMutex.Lock()
	if db.closed {
		db.closeMutex.Unlock()
		return nil
	}
	db.closed = true
	db.closeMutex.Unlock()

	if db.cancel != nil {
		db.cancel()
	}

	done := make(chan struct{})
	go func() {
		db.watcherWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		db.logger.Info("Database closed successfully")
		return nil
	case <-time.After(timeout):
		db.logger.Warn("Timeout waiting for file watcher to stop")
		return fmt.Errorf("timeout waiting for file watcher to stop")
	}
}
