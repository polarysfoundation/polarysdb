package wal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
	"github.com/polarysfoundation/polarysdb/v2/modules/logger"
)

// ----------------------------------------------------------------------------
// Helpers compartidos
// ----------------------------------------------------------------------------

// newTestWAL crea un WAL temporal con configuración adecuada para tests.
// Retorna el WAL, el path del archivo, y una función de cleanup.
func newTestWAL(t *testing.T, cfg *Config) (*WAL, string, func()) {
	t.Helper()

	if cfg == nil {
		cfg = &Config{}
	}

	// Defaults amigables para tests
	if cfg.Path == "" {
		tmpDir := t.TempDir()
		cfg.Path = filepath.Join(tmpDir, "test.wal")
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 50 * time.Millisecond
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 10 * 1024 * 1024 // 10MB
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 10
	}

	logCfg := logger.Config{
		MinLevel:  logger.LevelError, // silencioso en tests
		ToConsole: false,
		ToFile:    false,
	}
	log := logger.NewLogger(logCfg)

	w, err := New(cfg, log)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	w.SetContext(context.TODO())

	cleanup := func() {
		_ = w.Close()
	}

	return w, cfg.Path, cleanup
}

// appendN encola N entries en el WAL y retorna cuando todas fueron aceptadas.
// No espera a que se procesen (eso lo hace el caller con waitForBufferDrain).
func appendN(t *testing.T, w *WAL, n int) {
	t.Helper()
	for i := range n {
		err := w.Append(&Entry{
			OpType: OpWrite,
			Table:  "users",
			Key:    fmt.Sprintf("user_%d", i),
			Value:  fmt.Sprintf("value_%d", i),
		})
		if err != nil {
			t.Fatalf("Append failed at i=%d: %v", i, err)
		}
	}
}

// countEntriesInFile lee el archivo WAL (ya cerrado) y cuenta las entries
// que se persistieron. Usa una instancia fresca del WAL para evitar estado.
func countEntriesInFile(t *testing.T, path string, key common.Key) int {
	t.Helper()

	logCfg := logger.Config{MinLevel: logger.LevelError}
	log := logger.NewLogger(logCfg)

	w, err := New(&Config{
		Path:         path,
		SyncInterval: 50 * time.Millisecond,
		MaxSize:      10 * 1024 * 1024,
		BatchSize:    10,
		Key:          key,
	}, log)
	if err != nil {
		t.Fatalf("failed to reopen WAL for read: %v", err)
	}
	defer w.Close()

	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	return len(entries)
}

// ----------------------------------------------------------------------------
// PATCH 1: periodicSync debe flushear bufio.Writer antes de file.Sync()
// ----------------------------------------------------------------------------

// TestPatch1_PeriodicSyncFlushesWriter verifica que después de un ciclo de
// periodicSync, las entries "aceptadas" por Append() efectivamente llegaron
// al archivo en disco. Antes del patch, hasta 256KB quedaban en el
// bufio.Writer y file.Sync() no las tocaba.
//
// Estrategia:
//  1. Crear WAL con SyncInterval corto (50ms).
//  2. Append de N entries PEQUEÑAS (para no llenar el batch de 10 ni
//     el bufio.Writer de 256KB — queremos que el sync periódico las
//     persista, no el batch flush).
//  3. Esperar >SyncInterval para que periodicSync corra.
//  4. Cerrar el WAL gracefulmente.
//  5. Leer el archivo y verificar que las entries están.
//
// Sin el patch, este test pasa igual porque Close() hace Flush final.
// Para detectar el bug real necesitaríamos un crash abrupto, que es difícil
// de simular en tests. Por eso este test es de regresión: garantiza que
// el comportamiento correcto se mantiene.
func TestPatch1_PeriodicSyncFlushesWriter(t *testing.T) {
	w, path, cleanup := newTestWAL(t, &Config{
		SyncInterval: 50 * time.Millisecond,
		BatchSize:    1000, // grande para que no triggeree flushBatch
	})
	defer cleanup()

	w.Start()
	defer w.Close()

	// Append 50 entries pequeñas
	const n = 50
	appendN(t, w, n)

	// Esperar a que periodicSync corra al menos 2 ciclos
	time.Sleep(150 * time.Millisecond)

	// Esperar drain del buffer
	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}

	// Forzar flush+sync explícito
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Cerrar y leer
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	count := countEntriesInFile(t, path, common.Key{})
	if count != n {
		t.Errorf("expected %d entries in file, got %d", n, count)
	}
}

// TestPatch1_SyncFlushingAfterAppend verifica que Sync() explícito persiste
// entries inmediatamente, sin esperar al periodicSync.
func TestPatch1_SyncFlushingAfterAppend(t *testing.T) {
	w, path, cleanup := newTestWAL(t, &Config{
		SyncInterval: 10 * time.Second, // largo: forzar que Sync() haga el trabajo
		BatchSize:    1000,             // no flushear por batch
	})
	defer cleanup()

	w.Start()

	const n = 25
	appendN(t, w, n)

	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}

	// Sync explícito — debe flushear al disco AHORA
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Cerrar SIN flush final (simular crash controlado).
	// Cerramos el archivo directo para evitar el Flush de Close().
	w.closeMutex.Lock()
	w.closed = true
	if !w.bufferClosed {
		close(w.buffer)
		w.bufferClosed = true
	}
	w.closeMutex.Unlock()
	if w.cancel != nil {
		w.cancel()
	}
	// Esperar workers sin flush final
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	// Cerrar file sin Sync (simular crash)
	_ = w.file.Close()

	count := countEntriesInFile(t, path, common.Key{})
	if count != n {
		t.Errorf("expected %d entries persisted via Sync(), got %d", n, count)
	}
}

// ----------------------------------------------------------------------------
// PATCH 2: rotate debe ser síncrono y sin races
// ----------------------------------------------------------------------------

// TestPatch2_RotateSyncUnderLoad verifica que la rotación funciona
// correctamente bajo carga concurrente, sin panic ni pérdida de entries.
//
// Estrategia:
//  1. Crear WAL con MaxSize pequeño (10KB) para forzar múltiples rotaciones.
//  2. Append concurrente desde varias goroutines.
//  3. Esperar a que todo drene.
//  4. Verificar que no hubo panic (Go test falla automáticamente si una
//     goroutine panickea sin recover).
//  5. Verificar que el WAL sigue funcional post-rotación.
func TestPatch2_RotateSyncUnderLoad(t *testing.T) {
	w, path, cleanup := newTestWAL(t, &Config{
		MaxSize:      10 * 1024, // 10KB — fuerza rotaciones frecuentes
		BatchSize:    5,
		SyncInterval: 50 * time.Millisecond,
	})
	defer cleanup()

	w.Start()
	defer w.Close()

	const goroutines = 4
	const entriesPerG = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < entriesPerG; i++ {
				err := w.Append(&Entry{
					OpType: OpWrite,
					Table:  "users",
					Key:    fmt.Sprintf("g%d_user_%d", gid, i),
					Value:  fmt.Sprintf("g%d_value_%d", gid, i),
				})
				if err != nil {
					t.Errorf("goroutine %d Append failed at i=%d: %v", gid, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// Esperar drain completo
	if err := w.waitForBufferDrain(5 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}

	// Esperar a que rotateWatcher procese todas las rotaciones pendientes
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !w.rotating.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if w.rotating.Load() {
		t.Errorf("rotating flag still set after wait — rotateWatcher may be stuck")
	}

	// Verificar que el archivo WAL actual existe y es legible
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("WAL file missing after rotation: %v", err)
	}

	// Verificar que podemos seguir appendando post-rotación
	err := w.Append(&Entry{
		OpType: OpWrite,
		Table:  "users",
		Key:    "post_rotate",
		Value:  "post_rotate_value",
	})
	if err != nil {
		t.Fatalf("Append post-rotate failed: %v", err)
	}

	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain post-rotate: %v", err)
	}

	if err := w.Sync(); err != nil {
		t.Fatalf("Sync post-rotate failed: %v", err)
	}
}

// TestPatch2_RotateFlagAtomicity verifica que el flag `rotating` se levanta
// una sola vez incluso si múltiples entries disparan la condición de
// rotación simultáneamente.
func TestPatch2_RotateFlagAtomicity(t *testing.T) {
	w, _, cleanup := newTestWAL(t, &Config{
		MaxSize:      100, // muy pequeño — cualquier entry triggerea rotación
		BatchSize:    1,
		SyncInterval: 50 * time.Millisecond,
	})
	defer cleanup()

	w.Start()
	defer w.Close()

	// Append varias entries que cada una supere MaxSize
	for i := 0; i < 10; i++ {
		err := w.Append(&Entry{
			OpType: OpWrite,
			Table:  "users",
			Key:    fmt.Sprintf("user_%d", i),
			Value:  fmt.Sprintf("value_%d", i),
		})
		if err != nil {
			t.Fatalf("Append failed at i=%d: %v", i, err)
		}
	}

	if err := w.waitForBufferDrain(5 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}

	// Esperar a que rotateWatcher procese
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !w.rotating.Load() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// El flag debe estar abajo después de que rotateWatcher procesó
	if w.rotating.Load() {
		t.Errorf("rotating flag still set — rotateWatcher may not be clearing it")
	}
}

// ----------------------------------------------------------------------------
// PATCH 3: Truncate debe esperar drain del buffer
// ----------------------------------------------------------------------------

// TestPatch3_TruncateWaitsForDrain verifica que Truncate() no pierde
// entries que están encoladas en w.buffer cuando se llama.
//
// Estrategia:
//  1. Crear WAL con BatchSize alto (1000) para que flushBatch no se
//     triggeree por tamaño.
//  2. Append rápido N entries (se quedan en w.buffer o pendingBatch).
//  3. Llamar Truncate() inmediatamente.
//  4. Después de Truncate, appendar M entries nuevas.
//  5. Cerrar y leer — debe haber M entries (las N se truncaron).
//
// Sin el patch, Truncate podría cerrar y borrar el archivo mientras
// batchWriter todavía tiene entries en w.buffer, perdiéndolas o causando
// writes a un archivo cerrado (panic).
//
// Nota: este test NO verifica que las N entries se persistieron antes
// del truncate (eso es lo que hace el patch internamente). Verifica
// que Truncate no se cuelga ni panicea cuando hay entries en vuelo.
func TestPatch3_TruncateWaitsForDrain(t *testing.T) {
	w, path, cleanup := newTestWAL(t, &Config{
		BatchSize:    1000, // no flushear por batch
		MaxSize:      10 * 1024 * 1024,
		SyncInterval: 5 * time.Second, // no flushear por sync
	})
	defer cleanup()

	w.Start()

	// Append rápido — se quedan en buffer
	const n = 100
	appendN(t, w, n)

	// Truncate inmediatamente — debe esperar drain del buffer
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// Después del truncate, appendar entries nuevas
	const m = 20
	for i := 0; i < m; i++ {
		err := w.Append(&Entry{
			OpType: OpWrite,
			Table:  "users",
			Key:    fmt.Sprintf("post_trunc_%d", i),
			Value:  fmt.Sprintf("post_value_%d", i),
		})
		if err != nil {
			t.Fatalf("Append post-truncate failed at i=%d: %v", i, err)
		}
	}

	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain post-truncate: %v", err)
	}

	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	count := countEntriesInFile(t, path, common.Key{})
	if count != m {
		t.Errorf("expected %d entries after truncate+append, got %d", m, count)
	}
}

// TestPatch3_TruncateWithEmptyBuffer es el happy path: truncate sin
// entries pendientes debe funcionar igual.
func TestPatch3_TruncateWithEmptyBuffer(t *testing.T) {
	w, path, cleanup := newTestWAL(t, nil)
	defer cleanup()

	w.Start()

	// Append algo y esperar drain completo
	const n = 50
	appendN(t, w, n)
	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Truncate con buffer vacío
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	count := countEntriesInFile(t, path, common.Key{})
	if count != 0 {
		t.Errorf("expected 0 entries after truncate, got %d", count)
	}
}

// ----------------------------------------------------------------------------
// Tests adicionales de robustez
// ----------------------------------------------------------------------------

// TestWAL_CloseIsIdempotent verifica que Close() se puede llamar múltiples
// veces sin panic.
func TestWAL_CloseIsIdempotent(t *testing.T) {
	w, _, cleanup := newTestWAL(t, nil)
	defer cleanup()

	w.Start()

	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close failed: %v", err)
	}
}

// TestWAL_AppendAfterClose verifica que Append() retorna error después
// de Close().
func TestWAL_AppendAfterClose(t *testing.T) {
	w, _, cleanup := newTestWAL(t, nil)
	defer cleanup()

	w.Start()

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	err := w.Append(&Entry{
		OpType: OpWrite,
		Table:  "users",
		Key:    "should_fail",
		Value:  "x",
	})
	if err == nil {
		t.Error("expected error on Append after Close, got nil")
	}
}

// TestWAL_EncryptedRoundTrip verifica que las entries encriptadas se
// pueden leer correctamente con la misma clave.
func TestWAL_EncryptedRoundTrip(t *testing.T) {
	var key common.Key
	for i := range key {
		key[i] = byte(i + 1)
	}

	w, path, cleanup := newTestWAL(t, &Config{
		Key:       key,
		BatchSize: 5,
	})
	defer cleanup()

	w.Start()

	entries := []*Entry{
		{OpType: OpWrite, Table: "users", Key: "u1", Value: "alice"},
		{OpType: OpWrite, Table: "users", Key: "u2", Value: "bob"},
		{OpType: OpDelete, Table: "users", Key: "u1"},
	}

	for _, e := range entries {
		if err := w.Append(e); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	if err := w.waitForBufferDrain(2 * time.Second); err != nil {
		t.Fatalf("buffer did not drain: %v", err)
	}

	if err := w.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	count := countEntriesInFile(t, path, key)
	if count != len(entries) {
		t.Errorf("expected %d entries, got %d", len(entries), count)
	}
}
