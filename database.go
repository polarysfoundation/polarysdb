// ============================================================================
// FILE: polarysdb/database.go
// Core database implementation
// ============================================================================
package polarysdb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/backup"
	"github.com/polarysfoundation/polarysdb/modules/common"
	"github.com/polarysfoundation/polarysdb/modules/config"
	"github.com/polarysfoundation/polarysdb/modules/index"
	"github.com/polarysfoundation/polarysdb/modules/logger"
	"github.com/polarysfoundation/polarysdb/modules/mapper"
	"github.com/polarysfoundation/polarysdb/modules/metrics"
	"github.com/polarysfoundation/polarysdb/modules/storage"
	"github.com/polarysfoundation/polarysdb/modules/tx"
	"github.com/polarysfoundation/polarysdb/modules/wal"
)

const (
	maxBatchSize = 100000
)

const (
	OpCreate = "create"
	OpWrite  = "write"
	OpDelete = "delete"
	OpBatch  = "batch"
)

type WriteOperation struct {
	OpType    string
	Table     string
	Key       string
	Value     any
	Records   map[string]any
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
	lastLoaded atomic.Value
	lastSave   atomic.Value

	pendingOps atomic.Int64

	version atomic.Int64
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

	OpTimeout    time.Duration
	BatchTimeout time.Duration
}

// DefaultConfig retorna configuración por defecto
func DefaultConfig() *Config {
	dir := "data"
	return &Config{
		DirPath:            dir,
		BackupDir:          config.GetHomeSubDir("backups", dir),
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
		OpTimeout:          5 * time.Second,
		BatchTimeout:       30 * time.Second,
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

	// Cargar datos desde disco
	if err := db.loadWithRetry(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load database: %w", err)
	}

	// Inicializar WAL
	if cfg.EnableWAL {
		walPath := filepath.Join(config.GetHomeSubDir("", cfg.DirPath), "polarysdb.wal")
		walCfg := &wal.Config{
			Path:         walPath,
			SyncInterval: cfg.WALSyncInterval,
			MaxSize:      100 * 1024 * 1024,
		}

		db.wal, err = wal.New(walCfg, l)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize WAL: %w", err)
		}
		db.wal.SetContext(ctx)

		// Recuperar desde WAL — aplica entries sobre db.data ya cargado
		if err := db.recoverFromWAL(); err != nil {
			l.Warn("WAL recovery failed: ", err)
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
	// Workers simples: wg.Add(1) del loop exterior compensado por defer wg.Done() interno
	simpleWorkers := []func(){
		db.processWriteBuffer,
		db.periodicSave,
		db.fileOnChange,
	}

	for _, worker := range simpleWorkers {
		db.wg.Add(1)
		go worker()
	}

	// Workers con lifecycle propio: wg.Add/Done manejado internamente
	// NO agregar wg.Add(1) del loop — el worker lo maneja
	if db.config.EnableBackup && db.backupMgr != nil {
		db.wg.Add(1)
		go func() {
			defer db.wg.Done()
			db.backupMgr.Start(db.ctx, db.createBackupSnapshot)
		}()
	}

	if db.config.MetricsEnabled && db.metrics != nil {
		db.wg.Add(1)
		go func() {
			defer db.wg.Done()
			db.metrics.Start(db.ctx, db.logger)
		}()
	}

	if db.config.EnableWAL && db.wal != nil {
		db.wg.Add(1)
		go func() {
			defer db.wg.Done()
			db.wal.Start()
		}()
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
			db.logger.Warnf(
				"WAL recovery: table %q auto-created during OpWrite (missing OpCreate entry)",
				entry.Table,
			)
			db.data[entry.Table] = make(map[string]any)
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

func (db *Database) Exist(table string) (bool, error) {
	if db.closed.Load() {
		return false, fmt.Errorf("database is closed")
	}
	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()
	_, ok := db.data[table]
	return ok, nil
}

func (db *Database) Create(table string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	op := &WriteOperation{
		OpType:    OpCreate,
		Table:     table,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}
	db.pendingOps.Add(1)

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		db.pendingOps.Add(-1)
		return fmt.Errorf("database is shutting down")
	case <-time.After(db.config.OpTimeout):
		db.pendingOps.Add(-1)
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
		OpType:    OpWrite,
		Table:     table,
		Key:       key,
		Value:     value,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}
	db.pendingOps.Add(1)

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		db.pendingOps.Add(-1)
		return fmt.Errorf("database is shutting down")
	case <-time.After(db.config.OpTimeout):
		db.pendingOps.Add(-1)
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

	op := &WriteOperation{
		OpType:    OpBatch,
		Table:     table,
		Records:   records,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}
	db.pendingOps.Add(1)

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		db.pendingOps.Add(-1)
		return fmt.Errorf("database is shutting down")
	case <-time.After(db.config.BatchTimeout): // Timeout más largo para batches
		db.pendingOps.Add(-1)
		return fmt.Errorf("batch operation timeout")
	}
}

func (db *Database) Delete(table, key string) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	op := &WriteOperation{
		OpType:    OpDelete,
		Table:     table,
		Key:       key,
		ResultCh:  make(chan error, 1),
		Timestamp: time.Now(),
	}
	db.pendingOps.Add(1)

	select {
	case db.writeBuffer <- op:
		return <-op.ResultCh
	case <-db.ctx.Done():
		db.pendingOps.Add(-1)
		return fmt.Errorf("database is shutting down")
	case <-time.After(db.config.OpTimeout):
		db.pendingOps.Add(-1)
		return fmt.Errorf("operation timeout")
	}
}

func (db *Database) Read(table, key string, dst any) error {
	start := time.Now()
	defer func() {
		if db.metrics != nil {
			db.metrics.RecordReadLatency(time.Since(start))
		}
	}()

	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	if t, ok := db.data[table]; ok {
		value, exists := t[key]
		if !exists {
			return fmt.Errorf("data with key=%s does not exists", key)
		}

		if db.metrics != nil {
			db.metrics.IncrementReads()
		}

		if err := db.to(value, dst); err != nil {
			return err
		}

		return nil
	}

	return fmt.Errorf("table does not exist")
}

func (db *Database) ReadBatch(table string, dst any) error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	db.dataMutex.RLock()
	defer db.dataMutex.RUnlock()

	t, ok := db.data[table]
	if !ok {
		return fmt.Errorf("table %s does not exist", table)
	}

	// Validar que dst es un puntero no-nil a un slice
	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Ptr || dstVal.IsNil() {
		return fmt.Errorf("dst must be a non-nil pointer to a slice")
	}

	sliceVal := dstVal.Elem()
	if sliceVal.Kind() != reflect.Slice {
		return fmt.Errorf("dst must be a pointer to a slice, got pointer to %s", sliceVal.Kind())
	}

	elemType := sliceVal.Type().Elem()

	// Reset slice, pre-allocar capacidad
	results := reflect.MakeSlice(sliceVal.Type(), 0, len(t))

	for _, d := range t {
		src := deepCopyValue(d) // copia defensiva vs db.data interno

		switch elemType.Kind() {
		case reflect.Interface:
			// *[]any — appendar valor deep-copiado directamente
			if src == nil {
				results = reflect.Append(results, reflect.Zero(elemType))
			} else {
				results = reflect.Append(results, reflect.ValueOf(src))
			}

		case reflect.Pointer:
			// []*T — crear *T, mappear, appendar puntero
			concreteType := elemType.Elem()
			elemPtr := reflect.New(concreteType)
			if err := db.to(src, elemPtr.Interface()); err != nil {
				return fmt.Errorf("mapping error for key: %w", err)
			}
			results = reflect.Append(results, elemPtr)

		default:
			// []T — crear *T, mappear, appendar valor (no puntero)
			elemPtr := reflect.New(elemType)
			if err := db.to(src, elemPtr.Interface()); err != nil {
				return fmt.Errorf("mapping error for key: %w", err)
			}
			results = reflect.Append(results, elemPtr.Elem())
		}
	}

	// Asignar el slice resultante al destino
	sliceVal.Set(results)

	if db.metrics != nil {
		db.metrics.IncrementReads()
	}

	return nil
}

// Index operations
func (db *Database) CreateIndex(table, field string) error {
	if !db.config.EnableIndexes || db.indexMgr == nil {
		return fmt.Errorf("indexes are disabled")
	}

	db.dataMutex.RLock()
	tableData := db.data[table]
	db.dataMutex.RUnlock()

	if err := db.indexMgr.CreateIndex(table, field, tableData); err != nil {
		return err
	}

	return nil
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
	if db.closed.Load() {
		return nil, fmt.Errorf("database is closed")
	}

	if !db.config.EnableTransactions || db.txManager == nil {
		return nil, fmt.Errorf("transactions are disabled")
	}

	db.dataMutex.RLock()
	snapshot, version := db.createSnapshot()
	db.dataMutex.RUnlock()

	txn := db.txManager.Begin(snapshot)
	txn.SetSnapshotVersion(version)
	return txn, nil
}

func (db *Database) createSnapshot() (map[string]map[string]any, int64) {
	snapshot := make(map[string]map[string]any, len(db.data))
	for table, records := range db.data {
		snapshot[table] = make(map[string]any, len(records))
		for key, value := range records {
			snapshot[table][key] = deepCopyValue(value) // ← DEEP COPY
		}
	}
	return snapshot, db.version.Load() // ← capturar version
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

	snapshotVersion := txn.SnapshotVersion()
	currentVersion := db.version.Load()
	if currentVersion != snapshotVersion {
		return fmt.Errorf(
			"transaction conflict: data modified since snapshot (expected v%d, current v%d). Retry the transaction",
			snapshotVersion, currentVersion,
		)
	}

	// Aplicar cambios (sin cambios)
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

	db.version.Add(1)
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
		successCount := 0

		for _, op := range pendingOps {
			if op == nil {
				continue
			}

			if op.ResultCh == nil {
				continue
			}

			var err error
			switch op.OpType {
			case OpCreate:
				if _, ok := db.data[op.Table]; ok {
					err = fmt.Errorf("table %s already exists", op.Table)
				} else {
					db.data[op.Table] = make(map[string]any)
				}

				if db.wal != nil {
					if err := db.wal.Append(&wal.Entry{
						OpType: wal.OpCreate,
						Table:  op.Table,
					}); err != nil {
						db.logger.Errorf("WAL append failed: %v (op: %s/%s)", err, op.Table, op.Key)
						// Decisión: rollback la op o continuar sin WAL?
						// Recomendación: fail la op para garantizar durability
						err = fmt.Errorf("WAL create failed: %w", err)
						// Rollback: revertir la escritura en memoria
						delete(db.data, op.Table)
					}
				}
				successCount++
			case OpWrite:
				if t, ok := db.data[op.Table]; ok {
					oldVal := t[op.Key]
					t[op.Key] = op.Value

					// Update indexes
					if db.indexMgr != nil {
						fields := db.indexMgr.GetIndexedFields(op.Table)
						for _, field := range fields {
							db.indexMgr.UpdateIndex(op.Table, field, op.Key, oldVal, op.Value)
						}
					}

					if db.metrics != nil {
						db.metrics.IncrementWrites(1)
					}
					if db.wal != nil {
						if err := db.wal.Append(&wal.Entry{
							OpType: wal.OpWrite,
							Table:  op.Table,
							Key:    op.Key,
							Value:  op.Value,
						}); err != nil {
							db.logger.Errorf("WAL append failed: %v (op: %s/%s)", err, op.Table, op.Key)
							// Decisión: rollback la op o continuar sin WAL?
							// Recomendación: fail la op para garantizar durability
							err = fmt.Errorf("WAL write failed: %w", err)
							// Rollback: revertir la escritura en memoria
							t[op.Key] = oldVal
						}
					}
					successCount++
				} else {
					err = fmt.Errorf("table %s does not exist", op.Table)
				}

			case OpDelete:
				if t, ok := db.data[op.Table]; ok {
					oldVal := t[op.Key]
					delete(t, op.Key)

					// Update indexes
					if db.indexMgr != nil {
						fields := db.indexMgr.GetIndexedFields(op.Table)
						for _, field := range fields {
							db.indexMgr.DeleteFromIndex(op.Table, field, op.Key, oldVal)
						}
					}

					if db.metrics != nil {
						db.metrics.IncrementDeletes()
					}
					if db.wal != nil {
						if err := db.wal.Append(&wal.Entry{
							OpType: wal.OpDelete,
							Table:  op.Table,
							Key:    op.Key,
						}); err != nil {
							db.logger.Errorf("WAL append failed: %v (op: %s/%s)", err, op.Table, op.Key)
							// Decisión: rollback la op o continuar sin WAL?
							// Recomendación: fail la op para garantizar durability
							err = fmt.Errorf("WAL delete failed: %w", err)
							// Rollback: revertir la escritura en memoria
							t[op.Key] = oldVal
						}
					}
					successCount++
				} else {
					err = fmt.Errorf("table %s does not exist", op.Table)
				}
			case OpBatch:
				if t, ok := db.data[op.Table]; ok {
					for key, value := range op.Records {
						oldVal := t[key]
						t[key] = value

						// Actualizar índices
						if db.indexMgr != nil {
							fields := db.indexMgr.GetIndexedFields(op.Table)
							for _, field := range fields {
								db.indexMgr.UpdateIndex(op.Table, field, key, oldVal, value)
							}
						}

						// Append al WAL
						if db.wal != nil {
							if err := db.wal.Append(&wal.Entry{
								OpType: wal.OpWrite,
								Table:  op.Table,
								Key:    key,
								Value:  value,
							}); err != nil {
								db.logger.Errorf("WAL append failed: %v (op: %s/%s)", err, op.Table, op.Key)
								// Decisión: rollback la op o continuar sin WAL?
								// Recomendación: fail la op para garantizar durability
								err = fmt.Errorf("WAL write failed: %w", err)
								// Rollback: revertir la escritura en memoria
								t[op.Key] = oldVal
							}
						}
					}

					if db.metrics != nil {
						db.metrics.IncrementWrites(uint64(len(op.Records)))
					}
					successCount++

				} else {
					err = fmt.Errorf("table %s does not exist", op.Table)
				}
			default:
				err = fmt.Errorf("unknown operation type: %s", op.OpType)

			}

			if err != nil && db.metrics != nil {
				db.metrics.IncrementFailedOps()
			}
			db.pendingOps.Add(-1)

			select {
			case op.ResultCh <- err:
			default:
				// Canal cerrado o lleno, ignorar
			}
		}

		if successCount > 0 {
			db.version.Add(1)
			db.dirtyFlag.Store(true)
		}

		db.dataMutex.Unlock()
		pendingOps = pendingOps[:0]
	}

	for {
		// Primero intentar drain del canal (prioridad)
		select {
		case op, ok := <-db.writeBuffer:
			if !ok {
				processBatch()
				return // Canal cerrado y vacío → salir limpiamente
			}
			if op != nil {
				pendingOps = append(pendingOps, op)
				if len(pendingOps) >= 50 {
					processBatch()
				}
			}
		default:
			// Canal vacío por ahora → verificar context
			select {
			case <-db.ctx.Done():
				processBatch()
				return
			case op, ok := <-db.writeBuffer:
				if !ok {
					processBatch()
					return
				}
				if op != nil {
					pendingOps = append(pendingOps, op)
				}
			case <-batchTicker.C:
				processBatch()
			}
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

			lastLoaded, _ := db.lastLoaded.Load().(time.Time)
			if info.ModTime().Equal(lastLoaded) && !lastLoaded.IsZero() {
				continue
			}

			db.dataMutex.Lock()
			// Re-check dentro del lock — si dirty, skip
			if db.dirtyFlag.Load() || db.pendingOps.Load() > 0 {
				db.dataMutex.Unlock()
				continue
			}

			err = db.load()
			db.dataMutex.Unlock()

			if err != nil {
				db.logger.Warn("Error reloading database: ", err)
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
		db.dirtyFlag.CompareAndSwap(true, false)

		db.lastSave.Store(time.Now())

		if db.metrics != nil {
			db.metrics.RecordSaveDuration(time.Since(start))
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
	db.lastLoaded.Store(modTime)
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

	db.dataMutex.RLock()
	if !common.IsEqual(db.config.EncryptionKey[:], oldKey[:]) {
		db.dataMutex.RUnlock()
		return fmt.Errorf("old key does not match")
	}
	db.dataMutex.RUnlock()

	db.dataMutex.Lock()
	db.config.EncryptionKey = newKey
	db.dataMutex.Unlock()

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
	lastSave, _ := db.lastSave.Load().(time.Time)
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
		"last_save":         lastSave,
	}
}

// Close operations
func (db *Database) Close() error {
	return db.CloseWithTimeout(30 * time.Second)
}

// CloseWithTimeout cierra la base de datos con timeout mejorado
func (db *Database) CloseWithTimeout(timeout time.Duration) error {
	if !db.closed.CompareAndSwap(false, true) {
		return nil
	}

	db.logger.Info("Initiating database shutdown...")

	// 1. Cerrar buffer → processWriteBuffer draina todo antes de salir
	close(db.writeBuffer)

	// 2. Cancelar contexto (processWriteBuffer ya no lo usa para salir,
	//    solo otros workers)
	if db.cancel != nil {
		db.cancel()
	}

	// 4. Esperar workers con timeout
	done := make(chan struct{})
	go func() {
		db.wg.Wait()

		if db.dirtyFlag.Load() {
			db.flushToDisk()
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
		return fmt.Errorf("timeout waiting for database to close")
	}
}

func (db *Database) to(src any, dst any) error {
	if src == nil {
		return fmt.Errorf("source value is nil")
	}

	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Ptr || dstVal.IsNil() {
		return fmt.Errorf("dst must be a non-nil pointer")
	}

	srcVal := reflect.ValueOf(src)
	dstElem := dstVal.Elem()

	// Case 1: src y dst son el mismo tipo → deep copy
	// Handles: map[string]any→*map[string]any, []byte→*[]byte, int→*int, etc.
	if srcVal.Type() == dstElem.Type() {
		copied := deepCopyValue(src)
		copiedVal := reflect.ValueOf(copied)
		if copiedVal.Type() == dstElem.Type() {
			dstElem.Set(copiedVal)
			return nil
		}
	}

	// Case 2: map[string]any → struct via mapper
	dataMap, ok := src.(map[string]any)
	if ok {
		return mapper.MapToStruct(dataMap, dst)
	}

	// Case 3: Primitive direct assignment (compatible types)
	if dstElem.Kind() == srcVal.Kind() {
		dstElem.Set(srcVal)
		return nil
	}

	// Case 4: Fallback
	return mapper.ToStruct(src, dst)
}

// deepCopyValue realiza una copia profunda recursiva de cualquier valor.
// Los tipos primitivos (bool, int, string, etc.) se copian por valor naturalmente.
// Los tipos referencia (maps, slices) se copian recursivamente.
// Tipos desconocidos se delegan a JSON round-trip como fallback.
func deepCopyValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, string:
		return val
	case []byte:
		cp := make([]byte, len(val))
		copy(cp, val)
		return cp
	case map[string]any:
		cp := make(map[string]any, len(val))
		for k, child := range val {
			cp[k] = deepCopyValue(child)
		}
		return cp
	case []any:
		cp := make([]any, len(val))
		for i, child := range val {
			cp[i] = deepCopyValue(child)
		}
		return cp
	default:
		data, err := json.Marshal(val)
		if err != nil {
			return val
		}
		var result any
		if err := json.Unmarshal(data, &result); err != nil {
			return val
		}
		return result
	}
}
