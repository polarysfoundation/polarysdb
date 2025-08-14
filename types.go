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

	// Mejoras para el control del watcher
	ctx        context.Context
	cancel     context.CancelFunc
	watcherWg  sync.WaitGroup
	closed     bool
	closeMutex sync.Mutex
}

// Init initializes the database with the given encryption key and directory path.
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

	// Crear contexto con cancelación
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

	// Load for the first time
	if err := db.load(); err != nil {
		cancel() // Cancelar contexto si falla la carga inicial
		return nil, err
	}

	// Start the external change watcher con WaitGroup
	db.watcherWg.Add(1)
	go db.fileOnChange()

	return db, nil
}

// Exist checks if a table exists in the database.
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

// isClosed verifica si la base de datos está cerrada de forma thread-safe
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

			// Si el archivo no ha cambiado, continuar
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
func (db *Database) Close() error {
	db.closeMutex.Lock()
	if db.closed {
		db.closeMutex.Unlock()
		return nil // Ya está cerrada
	}
	db.closed = true
	db.closeMutex.Unlock()

	// Cancelar el contexto para señalar al watcher que debe detenerse
	if db.cancel != nil {
		db.cancel()
	}

	// Esperar con timeout a que termine el watcher
	done := make(chan struct{})
	go func() {
		db.watcherWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		db.logger.Info("Database closed successfully")
		return nil
	case <-time.After(10 * time.Second):
		db.logger.Warn("Timeout waiting for file watcher to stop")
		return fmt.Errorf("timeout waiting for file watcher to stop")
	}
}

// CloseWithTimeout permite especificar un timeout personalizado para el cierre
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
