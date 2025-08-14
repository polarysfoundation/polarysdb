package polarysdb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/common"
	"github.com/polarysfoundation/polarysdb/modules/config"
	"github.com/polarysfoundation/polarysdb/modules/crypto"
	"github.com/polarysfoundation/polarysdb/modules/logger"
)

// Registro global de instancias para debugging y cleanup
var (
	globalInstances = make(map[string]*Database)
	globalMutex     sync.RWMutex
	instanceCounter int64
)

// Database represents a simple in-memory database structure.
type Database struct {
	id         string // ID único para debugging
	dbPath     string
	data       map[string]map[string]any
	mutex      sync.RWMutex
	key        common.Key
	lastLoaded time.Time
	logger     *logger.Logger
	
	// Control del watcher
	ctx        context.Context
	cancel     context.CancelFunc
	watcherWg  sync.WaitGroup
	
	// Control de estado
	closed     int32  // usar atomic para mejor performance
	watcherStarted int32 // prevenir múltiples watchers
}

// GetActiveInstances devuelve el número de instancias activas (para debugging)
func GetActiveInstances() int {
	globalMutex.RLock()
	defer globalMutex.RUnlock()
	return len(globalInstances)
}

// ListActiveInstances devuelve los IDs de instancias activas (para debugging)
func ListActiveInstances() []string {
	globalMutex.RLock()
	defer globalMutex.RUnlock()
	
	ids := make([]string, 0, len(globalInstances))
	for id := range globalInstances {
		ids = append(ids, id)
	}
	return ids
}

// CloseAllInstances cierra todas las instancias activas (para cleanup en shutdown)
func CloseAllInstances() []error {
	globalMutex.RLock()
	instances := make([]*Database, 0, len(globalInstances))
	for _, db := range globalInstances {
		instances = append(instances, db)
	}
	globalMutex.RUnlock()
	
	var errors []error
	for _, db := range instances {
		if err := db.Close(); err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// Init initializes the database with the given encryption key and directory path.
func Init(keyDb common.Key, dirPath string) (*Database, error) {
	path := config.GetStateDBPath(dirPath)

	// Verificar si ya existe una instancia para esta ruta
	globalMutex.RLock()
	if existingDB, exists := globalInstances[path]; exists {
		globalMutex.RUnlock()
		if !existingDB.isClosed() {
			return nil, fmt.Errorf("database instance already exists for path: %s", path)
		}
		// Si existe pero está cerrada, la removemos del registro
		globalMutex.Lock()
		delete(globalInstances, path)
		globalMutex.Unlock()
	} else {
		globalMutex.RUnlock()
	}

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

	// Generar ID único
	id := fmt.Sprintf("db-%d-%s", atomic.AddInt64(&instanceCounter, 1), filepath.Base(path))

	db := &Database{
		id:     id,
		dbPath: path,
		data:   make(map[string]map[string]any),
		key:    keyDb,
		logger: l,
		ctx:    ctx,
		cancel: cancel,
	}

	// Load for the first time
	if err := db.load(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load database: %w", err)
	}

	// Registrar la instancia globalmente
	globalMutex.Lock()
	globalInstances[path] = db
	globalMutex.Unlock()

	// Start the external change watcher
	if atomic.CompareAndSwapInt32(&db.watcherStarted, 0, 1) {
		db.watcherWg.Add(1)
		go db.fileOnChange()
		db.logger.Info(fmt.Sprintf("Database %s initialized with watcher", db.id))
	}

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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return nil, fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
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
		return fmt.Errorf("database %s is closed", db.id)
	}
	
	db.mutex.Lock()
	defer db.mutex.Unlock()

	if !common.IsEqual(db.key[:], oldKey[:]) {
		return fmt.Errorf("old key does not match current database key")
	}

	db.key = newKey
	return db.save()
}

// GetID returns the unique ID of the database instance
func (db *Database) GetID() string {
	return db.id
}

// isClosed verifica si la base de datos está cerrada usando atomic
func (db *Database) isClosed() bool {
	return atomic.LoadInt32(&db.closed) != 0
}

// fileOnChange monitors the database file for external changes with proper cancellation.
func (db *Database) fileOnChange() {
	defer func() {
		db.watcherWg.Done()
		db.logger.Info(fmt.Sprintf("File watcher for %s stopped", db.id))
	}()
	
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	db.logger.Info(fmt.Sprintf("File watcher for %s started", db.id))

	for {
		select {
		case <-db.ctx.Done():
			db.logger.Info(fmt.Sprintf("Context cancelled for %s", db.id))
			return
			
		case <-ticker.C:
			if db.isClosed() {
				db.logger.Info(fmt.Sprintf("Database %s closed, stopping watcher", db.id))
				return
			}

			info, err := os.Stat(db.dbPath)
			if err != nil {
				if !os.IsNotExist(err) {
					db.logger.Error(fmt.Sprintf("Error stating database file for %s: %v", db.id, err))
				}
				continue
			}

			// Si el archivo no ha cambiado, continuar
			if info.ModTime().Equal(db.lastLoaded) && !db.lastLoaded.IsZero() {
				continue
			}

			// Intentar cargar los cambios con timeout
			loadCtx, loadCancel := context.WithTimeout(db.ctx, 5*time.Second)
			err = db.loadWithContext(loadCtx)
			loadCancel()

			if err != nil {
				if err == context.DeadlineExceeded {
					db.logger.Warn(fmt.Sprintf("Timeout loading database %s", db.id))
				} else {
					db.logger.Warn(fmt.Sprintf("Error reloading database %s: %v", db.id, err))
				}
			} else {
				db.logger.Info(fmt.Sprintf("Database %s reloaded successfully", db.id))
			}
		}
	}
}

// save serializes the database data to JSON, encrypts it, and writes it to the file.
func (db *Database) save() error {
	if db.isClosed() {
		return fmt.Errorf("database %s is closed", db.id)
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

// loadWithContext loads with context support for cancellation
func (db *Database) loadWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	
	go func() {
		db.mutex.Lock()
		defer db.mutex.Unlock()
		done <- db.load()
	}()
	
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
	// Usar atomic para marcar como cerrada
	if !atomic.CompareAndSwapInt32(&db.closed, 0, 1) {
		return nil // Ya está cerrada
	}

	db.logger.Info(fmt.Sprintf("Closing database %s", db.id))

	// Remover del registro global primero
	globalMutex.Lock()
	delete(globalInstances, db.dbPath)
	globalMutex.Unlock()

	// Cancelar el contexto
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
		db.logger.Info(fmt.Sprintf("Database %s closed successfully", db.id))
		return nil
	case <-time.After(15 * time.Second):
		db.logger.Warn(fmt.Sprintf("Timeout waiting for watcher to stop for %s", db.id))
		return fmt.Errorf("timeout waiting for file watcher to stop for database %s", db.id)
	}
}

// CloseWithTimeout permite especificar un timeout personalizado para el cierre
func (db *Database) CloseWithTimeout(timeout time.Duration) error {
	if !atomic.CompareAndSwapInt32(&db.closed, 0, 1) {
		return nil
	}

	db.logger.Info(fmt.Sprintf("Closing database %s with timeout %v", db.id, timeout))

	globalMutex.Lock()
	delete(globalInstances, db.dbPath)
	globalMutex.Unlock()

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
		db.logger.Info(fmt.Sprintf("Database %s closed successfully", db.id))
		return nil
	case <-time.After(timeout):
		db.logger.Warn(fmt.Sprintf("Timeout waiting for watcher to stop for %s", db.id))
		return fmt.Errorf("timeout waiting for file watcher to stop for database %s", db.id)
	}
}