// ============================================================================
// FILE: polarysdb/database.go
// Core database implementation
// ============================================================================
package polarysdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/backup"
	"github.com/polarysfoundation/polarysdb/modules/common"
	"github.com/polarysfoundation/polarysdb/modules/config"
	"github.com/polarysfoundation/polarysdb/modules/index"
	"github.com/polarysfoundation/polarysdb/modules/logger"
	"github.com/polarysfoundation/polarysdb/modules/metrics"
	"github.com/polarysfoundation/polarysdb/modules/storage"
	"github.com/polarysfoundation/polarysdb/modules/tx"
	"github.com/polarysfoundation/polarysdb/modules/wal"
)

const (
	maxBatchSize = 100000
)

// WriteOperation representa una operación de escritura pendiente
type WriteOperation struct {
	OpType    string
	Table     string
	Key       string
	Value     any
	ResultCh  chan error
	Timestamp time.Time
}

// Database representa la base de datos principal
type Database struct {
	// Core data
	data      map[string]map[string]any
	dataMutex sync.RWMutex

	// Components
	storage   *storage.Engine
	wal       *wal.WAL
	indexMgr  *index.Manager
	txManager *tx.Manager
	backupMgr *backup.Manager
	metrics   *metrics.Collector

	// Configuration
	config *Config
	logger *logger.Logger

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool

	// Buffers
	writeBuffer chan *WriteOperation

	// State
	dirtyFlag  atomic.Bool
	lastLoaded time.Time
	lastSave   time.Time
}

// Config configuración de la base de datos
type Config struct {
	// Paths
	DirPath   string
	BackupDir string

	// Security
	EncryptionKey common.Key

	// Features
	EnableWAL          bool
	EnableBackup       bool
	EnableIndexes      bool
	EnableTransactions bool
	EnableCompression  bool

	// Performance
	SaveInterval    time.Duration
	WALSyncInterval time.Duration
	WatchInterval   time.Duration
	BufferSize      int
	MaxConnections  int32

	// Reliability
	MaxRetries     int
	RetryDelay     time.Duration
	BackupInterval time.Duration

	// Monitoring
	Debug          bool
	MetricsEnabled bool
}

// DefaultConfig retorna configuración por defecto
func DefaultConfig() *Config {
	return &Config{
		DirPath:            "./data",
		BackupDir:          "./backups",
		SaveInterval:       5 * time.Second,
		WALSyncInterval:    1 * time.Second,
		WatchInterval:      3 * time.Second,
		BufferSize:         1000,
		MaxConnections:     1000,
		MaxRetries:         3,
		RetryDelay:         100 * time.Millisecond,
		BackupInterval:     1 * time.Hour,
		EnableWAL:          true,
		EnableBackup:       true,
		EnableIndexes:      true,
		EnableTransactions: true,
		EnableCompression:  false,
		Debug:              false,
		MetricsEnabled:     true,
	}
}

// Init inicializa la base de datos (mantiene compatibilidad)
func Init(keyDb common.Key, dirPath string, debug bool) (*Database, error) {
	cfg := DefaultConfig()
	cfg.DirPath = dirPath
	cfg.EncryptionKey = keyDb
	cfg.Debug = debug
	return InitWithConfig(cfg)
}

// InitWithConfig inicializa con configuración completa
func InitWithConfig(cfg *Config) (*Database, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if err := setupDirectories(cfg); err != nil {
		return nil, fmt.Errorf("failed to setup directories: %w", err)
	}

	// Inicializar logger
	logCfg := logger.Config{
		MinLevel:  logger.LevelInfo,
		ToConsole: true,
		ToFile:    false,
	}
	if cfg.Debug {
		logCfg.MinLevel = logger.LevelDebug
	}
	l := logger.NewLogger(logCfg)

	ctx, cancel := context.WithCancel(context.Background())

	// Crear storage engine
	storageEngine, err := storage.New(&storage.Config{
		DataPath:      config.GetStateDBPath(cfg.DirPath),
		EncryptionKey: cfg.EncryptionKey,
		Compression:   cfg.EnableCompression,
		MaxRetries:    cfg.MaxRetries,
		RetryDelay:    cfg.RetryDelay,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create storage engine: %w", err)
	}

	db := &Database{
		data:        make(map[string]map[string]any),
		storage:     storageEngine,
		config:      cfg,
		logger:      l,
		ctx:         ctx,
		cancel:      cancel,
		writeBuffer: make(chan *WriteOperation, cfg.BufferSize),
	}

	// Inicializar WAL
	if cfg.EnableWAL {
		walPath := filepath.Join(cfg.DirPath, "polarysdb.wal")
		walCfg := &wal.Config{
			Path:         walPath,
			SyncInterval: cfg.WALSyncInterval,
			MaxSize:      100 * 1024 * 1024, // 100MB
		}
		db.wal, err = wal.New(walCfg, l)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize WAL: %w", err)
		}

		// Recuperar desde WAL
		if err := db.recoverFromWAL(); err != nil {
			l.Warn("WAL recovery failed, loading from snapshot: ", err)
		}
	}

	// Inicializar índices
	if cfg.EnableIndexes {
		db.indexMgr = index.NewManager(l)
	}

	// Inicializar transaction manager
	if cfg.EnableTransactions {
		db.txManager = tx.NewManager(l)
	}

	// Inicializar backup manager
	if cfg.EnableBackup {
		backupCfg := &backup.Config{
			BackupDir: cfg.BackupDir,
			Interval:  cfg.BackupInterval,
			KeepCount: 10,
		}
		db.backupMgr = backup.NewManager(backupCfg, l)
	}

	// Inicializar métricas
	if cfg.MetricsEnabled {
		db.metrics = metrics.NewCollector()
	}

	// Cargar datos desde disco
	if err := db.loadWithRetry(); err != nil {
		cancel()
		if db.wal != nil {
			db.wal.Close()
		}
		return nil, fmt.Errorf("failed to load database: %w", err)
	}

	// Iniciar workers en background
	db.startBackgroundWorkers()

	l.Info("Database initialized successfully")
	return db, nil
}

func validateConfig(cfg *Config) error {
	if cfg.DirPath == "" {
		return fmt.Errorf("DirPath cannot be empty")
	}
	if cfg.SaveInterval < 100*time.Millisecond {
		return fmt.Errorf("SaveInterval too small")
	}
	if cfg.BufferSize < 10 {
		return fmt.Errorf("BufferSize too small")
	}
	return nil
}

func setupDirectories(cfg *Config) error {
	dirs := []string{cfg.DirPath}
	if cfg.EnableBackup {
		dirs = append(dirs, cfg.BackupDir)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (db *Database) startBackgroundWorkers() {
	workers := []func(){
		db.processWriteBuffer,
		db.periodicSave,
		db.fileOnChange,
	}

	if db.config.EnableWAL && db.wal != nil {
		workers = append(workers, db.wal.Start)
	}

	if db.config.EnableBackup && db.backupMgr != nil {
		workers = append(workers, func() {
			db.wg.Add(1)
			defer db.wg.Done()
			db.backupMgr.Start(db.ctx, db.createBackupSnapshot)
		})
	}

	if db.config.MetricsEnabled && db.metrics != nil {
		workers = append(workers, func() {
			db.wg.Add(1)
			defer db.wg.Done()
			db.metrics.Start(db.ctx, db.logger)
		})
	}

	for _, worker := range workers {
		db.wg.Add(1)
		go worker()
	}
}

// recoverFromWAL recupera el estado desde el WAL
func (db *Database) recoverFromWAL() error {
	if db.wal == nil {
		return nil
	}

	db.logger.Info("Starting WAL recovery...")
	entries, err := db.wal.ReadAll()
	if err != nil {
		return err
	}

	recovered := 0
	for _, entry := range entries {
		if err := db.applyWALEntry(entry); err != nil {
			db.logger.Warnf("Failed to apply WAL entry: %v", err)
			continue
		}
		recovered++
	}

	db.logger.Infof("WAL recovery complete. Recovered %d operations", recovered)
	return nil
}

func (db *Database) applyWALEntry(entry *wal.Entry) error {
	db.dataMutex.Lock()
	defer db.dataMutex.Unlock()

	switch entry.OpType {
	case wal.OpCreate:
		if _, ok := db.data[entry.Table]; !ok {
			db.data[entry.Table] = make(map[string]any)
		}
	case wal.OpWrite:
		if _, ok := db.data[entry.Table]; !ok {
			return fmt.Errorf("table %s does not exist", entry.Table)
		}
		db.data[entry.Table][entry.Key] = entry.Value
	case wal.OpDelete:
		if t, ok := db.data[entry.Table]; ok {
			delete(t, entry.Key)
		}
	}
	return nil
}

// CRUD Operations

func (db *Database) Exist(table string) bool {
	if db.closed.Load() {
		return false
	}
	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()
	_, ok := db.data[table]
	return ok
}

func (db *Database) Create(table string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	op := &WriteOperation{
		OpType:    "create",
		Table:     table,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		return fmt.Errorf("database is shutting down")
	case <-time.After(5 * time.Second):
		return fmt.Errorf("operation timeout")
	}
}

func (db *Database) Write(table, key string, value any) error {
	start := time.Now()
	defer func() {
		if db.metrics != nil {
			db.metrics.RecordWriteLatency(time.Since(start))
		}
	}()

	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	op := &WriteOperation{
		OpType:    "write",
		Table:     table,
		Key:       key,
		Value:     value,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		return fmt.Errorf("database is shutting down")
	case <-time.After(5 * time.Second):
		return fmt.Errorf("operation timeout")
	}
}

func (db *Database) WriteBatch(table string, records map[string]any) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}
	if len(records) > maxBatchSize {
		return fmt.Errorf("batch size exceeds maximum")
	}

	db.dataMutex.Lock()
	defer db.dataMutex.Unlock()

	if _, ok := db.data[table]; !ok {
		return fmt.Errorf("table %s does not exist", table)
	}

	for key, value := range records {
		db.data[table][key] = value
		if db.wal != nil {
			db.wal.Append(&wal.Entry{
				OpType: wal.OpWrite,
				Table:  table,
				Key:    key,
				Value:  value,
			})
		}
	}

	db.dirtyFlag.Store(true)
	if db.metrics != nil {
		db.metrics.IncrementWrites(uint64(len(records)))
	}

	return nil
}

func (db *Database) Delete(table, key string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	op := &WriteOperation{
		OpType:    "delete",
		Table:     table,
		Key:       key,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		return fmt.Errorf("database is shutting down")
	case <-time.After(5 * time.Second):
		return fmt.Errorf("operation timeout")
	}
}

func (db *Database) Read(table, key string) (any, bool) {
	start := time.Now()
	defer func() {
		if db.metrics != nil {
			db.metrics.RecordReadLatency(time.Since(start))
		}
	}()

	if db.closed.Load() {
		return nil, false
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	if t, ok := db.data[table]; ok {
		value, exists := t[key]
		if db.metrics != nil {
			db.metrics.IncrementReads()
		}
		return value, exists
	}
	return nil, false
}

func (db *Database) ReadBatch(table string) ([]any, error) {
	if db.closed.Load() {
		return nil, fmt.Errorf("database is closed")
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	t, ok := db.data[table]
	if !ok {
		return nil, fmt.Errorf("table %s does not exist", table)
	}

	v := make([]any, 0, len(t))
	for _, d := range t {
		v = append(v, d)
	}

	if db.metrics != nil {
		db.metrics.IncrementReads()
	}
	return v, nil
}

// Index operations

func (db *Database) CreateIndex(table, field string) error {
	if !db.config.EnableIndexes || db.indexMgr == nil {
		return fmt.Errorf("indexes are disabled")
	}

	db.dataMutex.RLock()
	tableData := db.data[table]
	db.dataMutex.RUnlock()

	return db.indexMgr.CreateIndex(table, field, tableData)
}

func (db *Database) QueryByIndex(table, field string, value any) ([]any, error) {
	if !db.config.EnableIndexes || db.indexMgr == nil {
		return nil, fmt.Errorf("indexes are disabled")
	}

	keys, err := db.indexMgr.Query(table, field, value)
	if err != nil {
		return nil, err
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	results := make([]any, 0, len(keys))
	if tableData, ok := db.data[table]; ok {
		for _, key := range keys {
			if val, ok := tableData[key]; ok {
				results = append(results, val)
			}
		}
	}

	return results, nil
}

// Transaction operations

func (db *Database) BeginTransaction() (*tx.Transaction, error) {
	if !db.config.EnableTransactions || db.txManager == nil {
		return nil, fmt.Errorf("transactions are disabled")
	}

	db.dataMutex.RLock()
	snapshot := db.createSnapshot()
	db.dataMutex.RUnlock()

	return db.txManager.Begin(snapshot), nil
}

func (db *Database) createSnapshot() map[string]map[string]any {
	snapshot := make(map[string]map[string]any)
	for table, records := range db.data {
		snapshot[table] = make(map[string]any)
		for key, value := range records {
			snapshot[table][key] = value
		}
	}
	return snapshot
}

func (db *Database) CommitTransaction(txn *tx.Transaction) error {
	if db.txManager == nil {
		return fmt.Errorf("transactions are disabled")
	}

	changes, err := db.txManager.Commit(txn)
	if err != nil {
		return err
	}

	db.dataMutex.Lock()
	defer db.dataMutex.Unlock()

	for table, records := range changes {
		if _, ok := db.data[table]; !ok {
			db.data[table] = make(map[string]any)
		}
		for key, value := range records {
			if value == nil {
				delete(db.data[table], key)
			} else {
				db.data[table][key] = value
			}
		}
	}

	db.dirtyFlag.Store(true)
	return nil
}

// Background workers
func (db *Database) processWriteBuffer() {
	defer db.wg.Done()

	batchTicker := time.NewTicker(100 * time.Millisecond)
	defer batchTicker.Stop()

	pendingOps := make([]*WriteOperation, 0, 100)

	processBatch := func() {
		if len(pendingOps) == 0 {
			return
		}

		db.dataMutex.Lock()
		for _, op := range pendingOps {
			// ✅ FIX: Verificar que op no sea nil
			if op == nil {
				continue
			}

			// ✅ FIX: Verificar que ResultCh no sea nil
			if op.ResultCh == nil {
				continue
			}

			var err error
			switch op.OpType {
			case "create":
				if _, ok := db.data[op.Table]; !ok {
					db.data[op.Table] = make(map[string]any)
				}
				if db.wal != nil {
					db.wal.Append(&wal.Entry{
						OpType: wal.OpCreate,
						Table:  op.Table,
					})
				}

			case "write":
				if t, ok := db.data[op.Table]; ok {
					t[op.Key] = op.Value
					if db.metrics != nil {
						db.metrics.IncrementWrites(1)
					}
					if db.wal != nil {
						db.wal.Append(&wal.Entry{
							OpType: wal.OpWrite,
							Table:  op.Table,
							Key:    op.Key,
							Value:  op.Value,
						})
					}
				} else {
					err = fmt.Errorf("table %s does not exist", op.Table)
				}

			case "delete":
				if t, ok := db.data[op.Table]; ok {
					delete(t, op.Key)
					if db.metrics != nil {
						db.metrics.IncrementDeletes()
					}
					if db.wal != nil {
						db.wal.Append(&wal.Entry{
							OpType: wal.OpDelete,
							Table:  op.Table,
							Key:    op.Key,
						})
					}
				} else {
					err = fmt.Errorf("table %s does not exist", op.Table)
				}
			}

			if err != nil && db.metrics != nil {
				db.metrics.IncrementFailedOps()
			}

			// ✅ FIX: Enviar error solo si el canal no está cerrado
			select {
			case op.ResultCh <- err:
			default:
				// Canal cerrado o lleno, ignorar
			}
		}
		db.dirtyFlag.Store(true)
		db.dataMutex.Unlock()

		pendingOps = pendingOps[:0]
	}

	for {
		select {
		case <-db.ctx.Done():
			processBatch()
			return
		case op, ok := <-db.writeBuffer:
			// ✅ FIX: Verificar que el canal no esté cerrado
			if !ok {
				processBatch()
				return
			}
			// ✅ FIX: Verificar que op no sea nil
			if op != nil {
				pendingOps = append(pendingOps, op)
				if len(pendingOps) >= 50 {
					processBatch()
				}
			}
		case <-batchTicker.C:
			processBatch()
		}
	}
}

func (db *Database) periodicSave() {
	defer db.wg.Done()

	ticker := time.NewTicker(db.config.SaveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-db.ctx.Done():
			if db.dirtyFlag.Load() {
				db.flushToDisk()
			}
			return
		case <-ticker.C:
			if db.dirtyFlag.Load() {
				if err := db.flushToDisk(); err != nil {
					db.logger.Error("Periodic save failed: ", err)
					if db.metrics != nil {
						db.metrics.IncrementFailedOps()
					}
				}
			}
		}
	}
}

func (db *Database) fileOnChange() {
	defer db.wg.Done()

	ticker := time.NewTicker(db.config.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-db.ctx.Done():
			return
		case <-ticker.C:
			if db.closed.Load() {
				return
			}

			info, err := os.Stat(db.storage.GetPath())
			if err != nil {
				continue
			}

			if info.ModTime().Equal(db.lastLoaded) && !db.lastLoaded.IsZero() {
				continue
			}

			if !db.dirtyFlag.Load() && len(db.writeBuffer) == 0 {
				db.dataMutex.Lock()
				err = db.load()
				db.dataMutex.Unlock()

				if err != nil {
					db.logger.Warn("Error reloading database: ", err)
				}
			}
		}
	}
}

// Storage operations

func (db *Database) flushToDisk() error {
	start := time.Now()

	db.dataMutex.RLock()
	err := db.storage.Save(db.data)
	db.dataMutex.RUnlock()

	if err == nil {
		db.dirtyFlag.Store(false)
		db.lastSave = time.Now()

		if db.metrics != nil {
			db.metrics.RecordSaveDuration(time.Since(start))
		}

		if db.config.Debug {
			db.logger.Infof("Database saved in %v", time.Since(start))
		}
	}

	return err
}

func (db *Database) loadWithRetry() error {
	for i := 0; i < db.config.MaxRetries; i++ {
		if err := db.load(); err != nil {
			db.logger.Warnf("Load attempt %d failed: %v", i+1, err)
			time.Sleep(db.config.RetryDelay * time.Duration(i+1))
			continue
		}
		return nil
	}
	return fmt.Errorf("failed after %d retries", db.config.MaxRetries)
}

func (db *Database) load() error {
	data, modTime, err := db.storage.Load()
	if err != nil {
		return err
	}

	db.data = data
	db.lastLoaded = modTime
	return nil
}

func (db *Database) createBackupSnapshot() ([]byte, error) {
	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()
	return db.storage.Serialize(db.data)
}

// Import/Export operations

func (db *Database) Export(key common.Key, path string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	if !common.IsEqual(key[:], db.config.EncryptionKey[:]) {
		return fmt.Errorf("unauthorized access")
	}

	return db.storage.ExportPlain(db.data, path)
}

func (db *Database) Import(key common.Key, path string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	if !common.IsEqual(key[:], db.config.EncryptionKey[:]) {
		return fmt.Errorf("unauthorized access")
	}

	data, err := db.storage.ImportPlain(path)
	if err != nil {
		return err
	}

	db.dataMutex.Lock()
	db.data = data
	db.dirtyFlag.Store(true)
	db.dataMutex.Unlock()

	return db.flushToDisk()
}

func (db *Database) ExportEncrypted(key common.Key, path string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	if !common.IsEqual(key[:], db.config.EncryptionKey[:]) {
		return fmt.Errorf("unauthorized access")
	}

	return db.storage.ExportEncrypted(db.data, path)
}

func (db *Database) ImportEncrypted(key common.Key, path string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	if !common.IsEqual(key[:], db.config.EncryptionKey[:]) {
		return fmt.Errorf("unauthorized access")
	}

	data, err := db.storage.ImportEncrypted(path)
	if err != nil {
		return err
	}

	db.dataMutex.Lock()
	db.data = data
	db.dirtyFlag.Store(true)
	db.dataMutex.Unlock()

	return db.flushToDisk()
}

func (db *Database) ChangeKey(oldKey, newKey common.Key) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	if !common.IsEqual(db.config.EncryptionKey[:], oldKey[:]) {
		return fmt.Errorf("old key does not match")
	}

	db.config.EncryptionKey = newKey
	db.storage.UpdateKey(newKey)
	db.dirtyFlag.Store(true)

	return db.flushToDisk()
}

// Metrics and monitoring

func (db *Database) GetMetrics() *metrics.Snapshot {
	if db.metrics == nil {
		return &metrics.Snapshot{}
	}
	return db.metrics.GetSnapshot()
}

func (db *Database) GetStatus() map[string]any {
	m := db.GetMetrics()
	return map[string]any{
		"uptime_seconds":    time.Since(m.Uptime).Seconds(),
		"closed":            db.closed.Load(),
		"dirty":             db.dirtyFlag.Load(),
		"total_reads":       m.TotalReads,
		"total_writes":      m.TotalWrites,
		"total_deletes":     m.TotalDeletes,
		"failed_ops":        m.FailedOps,
		"avg_read_latency":  m.AvgReadLatency,
		"avg_write_latency": m.AvgWriteLatency,
		"buffered_ops":      len(db.writeBuffer),
		"last_save":         db.lastSave,
	}
}

// Close operations

func (db *Database) Close() error {
	return db.CloseWithTimeout(30 * time.Second)
}

func (db *Database) CloseWithTimeout(timeout time.Duration) error {
	if !db.closed.CompareAndSwap(false, true) {
		return nil
	}

	db.logger.Info("Initiating database shutdown...")

	close(db.writeBuffer)
	if db.cancel != nil {
		db.cancel()
	}

	done := make(chan struct{})
	go func() {
		db.wg.Wait()

		if db.dirtyFlag.Load() {
			if err := db.flushToDisk(); err != nil {
				db.logger.Error("Failed to save on close: ", err)
			}
		}

		if db.wal != nil {
			db.wal.Close()
		}

		close(done)
	}()

	select {
	case <-done:
		db.logger.Info("Database closed successfully")
		return nil
	case <-time.After(timeout):
		db.logger.Warn("Timeout waiting for database to close")
		return fmt.Errorf("timeout waiting for database to close")
	}
}
