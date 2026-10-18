package polarysdb

import (
	"os"
	"testing"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
)

// ============================================================================
// Shared test keys
// ============================================================================
//
// These are used by tests that need to call ChangeKey, Export, Import, etc.
// directly with explicit keys. newTestDB uses testKey as the default
// encryption key, so a DB created via newTestDB has testKey as its key.

var testKey = common.BytesToKey([]byte("test_password_for_test_32bytes"))
var testKey2 = common.BytesToKey([]byte("test_password_for_test_2_32bytes"))

// ============================================================================
// newTestDB — canonical test helper
// ============================================================================
//
// Creates a Database with:
//   - DirPath under t.TempDir() (absolute, auto-cleaned)
//   - WAL + storage + backups ALL under DirPath (no leakage to home/)
//   - EncryptionKey = testKey (deterministic for reproducibility)
//   - Reasonable defaults for tests (short intervals for fast feedback)
//
// Caller can pass a custom *Config to override any field. If nil, uses
// DefaultConfig. Any zero-value field is filled with a test-friendly
// default.
//
// REQUIRES (already applied in your repo):
//   - config.go fix: GetHomeSubDir respects absolute paths.
//   - database.go patch: InitWithConfig recalculates BackupDir when DirPath
//     changes from the default "data".
//
// Returns (db, cleanup). The cleanup closes the DB and removes DirPath.
// Caller MUST defer cleanup() (or use t.Cleanup).

func newTestDB(t *testing.T, cfg *Config) (*Database, func()) {
	t.Helper()

	if cfg == nil {
		cfg = DefaultConfig()
	}

	// SIEMPRE usar un temp dir absoluto para tests, sin importar qué
	// trajera cfg.DirPath. Si el caller ya pasó un path propio, lo
	// respetamos (puede estar testeando un Path específico).
	if cfg.DirPath == "" || cfg.DirPath == "data" {
		cfg.DirPath = t.TempDir()
	}

	// Defaults amigables para tests (solo se aplican si el caller dejó
	// el campo en zero-value).
	if cfg.SaveInterval == 0 {
		cfg.SaveInterval = 100 * time.Millisecond
	}
	if cfg.WALSyncInterval == 0 {
		cfg.WALSyncInterval = 50 * time.Millisecond
	}
	if cfg.WatchInterval == 0 {
		cfg.WatchInterval = 100 * time.Millisecond
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 100
	}
	if cfg.OpTimeout == 0 {
		cfg.OpTimeout = 2 * time.Second
	}
	if cfg.BatchTimeout == 0 {
		cfg.BatchTimeout = 5 * time.Second
	}
	if cfg.BackupInterval == 0 {
		cfg.BackupInterval = 1 * time.Hour
	}

	// Generar clave determinística para reproducibilidad.
	// Si el caller ya seteó EncryptionKey, respetarlo.
	if cfg.EncryptionKey.IsZero() {
		cfg.EncryptionKey = testKey
	}

	// NO overridear BackupDir — InitWithConfig lo recalcula automáticamente
	// cuando detecta que cambiamos DirPath (requiere patch en database.go).

	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("InitWithConfig failed: %v", err)
	}

	cleanup := func() {
		// Cerrar antes de borrar archivos.
		_ = db.Close()
		// Borrar TODO bajo DirPath (state, WAL, backups, todo).
		_ = os.RemoveAll(cfg.DirPath)
	}

	return db, cleanup
}

// ============================================================================
// Convenience wrappers
// ============================================================================

// newTestDBNoWAL crea una Database con WAL deshabilitado e intervals
// LARGOS (1 hora) para evitar que periodicSave o fileOnChange interfieran
// con tests que verifican estado interno o versiones.
//
// Úselo para tests que:
//   - Verifican dirtyFlag, version, pendingOps, etc.
//   - Necesitan control exacto de cuándo se flushea a disco.
//   - No quieren la complejidad del WAL en el camino.
//
// Para tests que SÍ necesitan WAL (recovery, persistencia cross-restart),
// use newTestDBWithWAL.
func newTestDBNoWAL(t *testing.T) (*Database, func()) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.EnableWAL = false
	cfg.EnableBackup = false
	cfg.EnableCompression = false
	// Intervals largos para evitar interferencia con assertions.
	cfg.SaveInterval = 1 * time.Hour
	cfg.WatchInterval = 1 * time.Hour
	cfg.BackupInterval = 1 * time.Hour
	return newTestDB(t, cfg)
}

// newTestDBWithWAL crea una Database con WAL habilitado y sync interval
// corto para tests de recovery y persistencia.
func newTestDBWithWAL(t *testing.T) (*Database, func()) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.EnableWAL = true
	cfg.EnableBackup = false // no necesitamos backups en tests de WAL
	cfg.WALSyncInterval = 100 * time.Millisecond
	return newTestDB(t, cfg)
}

// newTestDBWithConfig crea una Database permitiendo al caller modificar
// la config antes de inicializar. Útil para tests que necesitan settings
// específicos (ej. SaveInterval corto, EnableTransactions=false).
//
// Ejemplo:
//
//	db, cleanup := newTestDBWithConfig(t, func(cfg *Config) {
//	    cfg.SaveInterval = 200 * time.Millisecond
//	    cfg.EnableTransactions = false
//	})
//	defer cleanup()
func newTestDBWithConfig(t *testing.T, modify func(*Config)) (*Database, func()) {
	t.Helper()
	cfg := DefaultConfig()
	if modify != nil {
		modify(cfg)
	}
	return newTestDB(t, cfg)
}
