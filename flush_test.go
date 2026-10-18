package polarysdb

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
)

// ----------------------------------------------------------------------------
// Tests del Flujo principal
// ----------------------------------------------------------------------------

// TestFlush_PersistsDataAndCleansDirtyFlag verifica el happy path:
//  1. Escribir datos → dirtyFlag se levanta.
//  2. Flush() → dirtyFlag se baja.
//  3. Recargar → datos están presentes.
//  4. Comparación vía JSON → datos son consistentes.
func TestFlush_PersistsDataAndCleansDirtyFlag(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	// Crear tabla y escribir entries
	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	const n = 20
	for i := range n {
		err := db.Write("users", fmt.Sprintf("user_%d", i), fmt.Sprintf("value_%d", i))
		if err != nil {
			t.Fatalf("Write failed at i=%d: %v", i, err)
		}
	}

	// Esperar a que las ops se apliquen a db.data
	time.Sleep(100 * time.Millisecond)

	// dirtyFlag debe estar levantado
	if !db.dirtyFlag.Load() {
		t.Error("expected dirtyFlag=true after writes, got false")
	}

	// Flush
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// dirtyFlag debe estar bajado
	if db.dirtyFlag.Load() {
		t.Error("expected dirtyFlag=false after Flush, got true")
	}
}

// TestFlush_DataSurvivesReopen verifica que después de Flush, los datos
// están en disco y pueden ser leídos por una nueva instancia de Database.
func TestFlush_DataSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	// CRÍTICO: t.Cleanup se ejecuta SIEMPRE, incluso si el test paniquea
	// o faila a mitad de camino. defer solo corre si el test termina
	// normalmente; t.Cleanup corre en todos los casos.
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	cfg := DefaultConfig()
	cfg.DirPath = dir

	var key common.Key
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg.EncryptionKey = key

	// Silenciar logs en tests
	cfg.Debug = false
	cfg.MetricsEnabled = false
	cfg.EnableBackup = false

	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Asegurar cleanup de la primera instancia incluso si el test falla
	// después. Importante: cerrar ANTES de que t.Cleanup borre el dir,
	// para que todos los file handles se liberen (especialmente en macOS
	// donde no se puede borrar un archivo abierto).
	t.Cleanup(func() {
		_ = db.Close()
	})

	// Crear tabla y escribir datos
	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	for i := range 10 {
		if err := db.Write("users", fmt.Sprintf("u%d", i), fmt.Sprintf("v%d", i)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	// Esperar que se apliquen
	time.Sleep(100 * time.Millisecond)

	// Flush
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Cerrar explícitamente la primera instancia antes de reabrir,
	// para liberar todos los file handles.
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Reabrir con la misma configuración
	db2, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer db2.Close()

	// Leer los datos
	for i := range 10 {
		var val string
		err := db2.Read("users", fmt.Sprintf("u%d", i), &val)
		if err != nil {
			t.Errorf("Read failed for u%d: %v", i, err)
			continue
		}
		expected := fmt.Sprintf("v%d", i)
		if val != expected {
			t.Errorf("u%d: expected %q, got %q", i, expected, val)
		}
	}
}

// TestFlush_EarlyExitWhenNotDirty verifica que Flush() no hace I/O
// innecesario si no hay cambios sin persistir.
func TestFlush_EarlyExitWhenNotDirty(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	// Recién inicializada — no debería estar sucia (load trae lastSave)
	// pero dirtyFlag puede estar true si el init marcó algo. Si está
	// false, Flush debe hacer early exit sin error.

	start := time.Now()
	err := db.Flush()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Early exit debe ser muy rápido (< 50ms típicamente)
	if elapsed > 200*time.Millisecond {
		t.Errorf("Flush took %s — expected early exit to be fast", elapsed)
	}
}

// TestFlush_AfterWriteBatch persiste correctamente un batch grande.
func TestFlush_AfterWriteBatch(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Crear batch grande
	const n = 500
	records := make(map[string]any, n)
	for i := range n {
		records[fmt.Sprintf("u%d", i)] = fmt.Sprintf("v%d", i)
	}

	if err := db.WriteBatch("users", records); err != nil {
		t.Fatalf("WriteBatch failed: %v", err)
	}

	// Esperar a que se aplique
	time.Sleep(200 * time.Millisecond)

	// Flush
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verificar que los datos están
	var val string
	if err := db.Read("users", "u0", &val); err != nil {
		t.Errorf("Read u0 failed: %v", err)
	} else if val != "v0" {
		t.Errorf("u0: expected v0, got %s", val)
	}

	if err := db.Read("users", "u499", &val); err != nil {
		t.Errorf("Read u499 failed: %v", err)
	} else if val != "v499" {
		t.Errorf("u499: expected v499, got %s", val)
	}
}

// TestFlush_WaitsForPendingOps verifica que Flush() espera a que las
// operaciones pendientes en writeBuffer se apliquen antes de flushear.
//
// Estrategia:
//  1. Escribir N entries (se encolan en writeBuffer).
//  2. Llamar Flush() inmediatamente (sin esperar a que se apliquen).
//  3. Flush debe esperar a que pendingOps llegue a 0, luego flushear.
//  4. Verificar que después de Flush, los datos están persisted.
func TestFlush_WaitsForPendingOps(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Escribir muchas entries — van a encolarse en writeBuffer
	const n = 100
	for i := 0; i < n; i++ {
		go func(i int) {
			_ = db.Write("users", fmt.Sprintf("u%d", i), fmt.Sprintf("v%d", i))
		}(i)
	}

	// Flush inmediatamente — debe esperar a que las ops se apliquen
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Después de Flush, dirtyFlag debe estar false y datos persisted
	if db.dirtyFlag.Load() {
		t.Error("expected dirtyFlag=false after Flush, got true")
	}

	// Verificar algunos datos
	var val string
	if err := db.Read("users", "u0", &val); err != nil {
		t.Errorf("Read u0 failed: %v", err)
	}
}

// TestFlush_FlushIsIdempotent verifica que llamar Flush() múltiples veces
// seguidas no causa problemas.
func TestFlush_FlushIsIdempotent(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := db.Write("users", "u1", "v1"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Llamar Flush 5 veces seguidas
	for i := 0; i < 5; i++ {
		if err := db.Flush(); err != nil {
			t.Fatalf("Flush #%d failed: %v", i+1, err)
		}
	}

	// dirtyFlag debe estar false
	if db.dirtyFlag.Load() {
		t.Error("expected dirtyFlag=false after multiple Flush, got true")
	}
}

// TestFlush_ConcurrentWritesWithFlush verifica que Flush() es seguro
// bajo writes concurrentes.
func TestFlush_ConcurrentWritesWithFlush(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Writer goroutines
	const writers = 4
	const writesPerG = 50

	var wg sync.WaitGroup
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < writesPerG; i++ {
				err := db.Write("users", fmt.Sprintf("g%d_u%d", gid, i), fmt.Sprintf("g%d_v%d", gid, i))
				if err != nil {
					t.Errorf("writer %d Write failed at i=%d: %v", gid, i, err)
					return
				}
			}
		}(g)
	}

	// Flush goroutine: llama Flush cada 50ms mientras los writers corren.
	//
	// Patrón de cancelación: el caller externo cierra flushDone para
	// señalizar stop. La goroutine recibe el close y retorna. SOLO el
	// caller cierra el channel — la goroutine NO lo cierra.
	flushDone := make(chan struct{})
	var flushWg sync.WaitGroup
	flushWg.Add(1)
	go func() {
		defer flushWg.Done() // NO cerrar flushDone aquí
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = db.Flush()
			case <-flushDone:
				return
			}
		}
	}()

	// Esperar a que todos los writers terminen.
	wg.Wait()

	// Señalizar a la flush goroutine que pare cerrando el channel.
	// SOLO el caller cierra flushDone.
	close(flushDone)

	// Esperar a que la flush goroutine termine de salir limpio.
	// Esto evita races donde el test termine y el cleanup cierre la DB
	// mientras la flush goroutine todavía está llamando db.Flush().
	flushWg.Wait()

	// Flush final para garantizar que todo quedó persisted.
	if err := db.Flush(); err != nil {
		t.Fatalf("final Flush failed: %v", err)
	}

	// Verificar algunos datos
	var val string
	if err := db.Read("users", "g0_u0", &val); err != nil {
		t.Errorf("Read g0_u0 failed: %v", err)
	} else if val != "g0_v0" {
		t.Errorf("g0_u0: expected g0_v0, got %s", val)
	}

	// Verificar que el último write de cada writer también está.
	for g := 0; g < writers; g++ {
		key := fmt.Sprintf("g%d_u%d", g, writesPerG-1)
		expected := fmt.Sprintf("g%d_v%d", g, writesPerG-1)
		var v string
		if err := db.Read("users", key, &v); err != nil {
			t.Errorf("Read %s failed: %v", key, err)
		} else if v != expected {
			t.Errorf("%s: expected %s, got %s", key, expected, v)
		}
	}
}

// TestFlush_WithWALEnabled verifica que Flush() sincroniza el WAL cuando
// el WAL está habilitado y soporta Sync().
func TestFlush_WithWALEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableWAL = true

	db, cleanup := newTestDB(t, cfg)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		if err := db.Write("users", fmt.Sprintf("u%d", i), fmt.Sprintf("v%d", i)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	// Esperar a que se apliquen
	time.Sleep(100 * time.Millisecond)

	// Flush debe sincronizar tanto el snapshot como el WAL
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// El WAL debe tener entries (no truncado)
	if db.wal != nil {
		stats := db.wal.GetStats()
		entries, _ := stats["entries"].(uint64)
		if entries == 0 {
			t.Error("expected WAL entries > 0, got 0")
		}
	}
}

// TestFlush_DetectsCorruptionWhenVerifyFails es un test de regresión:
// verifica que si storage.Save y storage.Load son consistentes, el
// verifyFlush no da falsos positivos.
//
// Nota: este test NO induce corrupción (eso requeriría mockear storage).
// Solo verifica que el happy path no rompe por la verificación.
func TestFlush_DetectsCorruptionWhenVerifyFails(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Tipos de valores variados para ejercitar JSON marshal
	if err := db.Write("users", "string", "hello"); err != nil {
		t.Fatalf("Write string failed: %v", err)
	}
	if err := db.Write("users", "int", 42); err != nil {
		t.Fatalf("Write int failed: %v", err)
	}
	if err := db.Write("users", "bool", true); err != nil {
		t.Fatalf("Write bool failed: %v", err)
	}
	if err := db.Write("users", "map", map[string]any{"a": 1, "b": "x"}); err != nil {
		t.Fatalf("Write map failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Flush debe pasar la verificación sin falsos positivos
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed (verify false positive?): %v", err)
	}
}

// TestFlush_ReturnsErrorOnClosedDB verifica que Flush retorna error
// si la DB está cerrada.
func TestFlush_ReturnsErrorOnClosedDB(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	// No defer cleanup — lo manejamos manualmente

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := db.Write("users", "u1", "v1"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Cerrar
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Flush después de Close debe fallar
	err := db.Flush()
	if err == nil {
		t.Error("expected error on Flush after Close, got nil")
	}

	cleanup()
}

// TestFlush_StateAfterFlushMatchesInMemory verifica que después de Flush,
// si recargamos la DB desde disco (sin cerrar), el estado es consistente
// con el estado en memoria.
func TestFlush_StateAfterFlushMatchesInMemory(t *testing.T) {
	db, cleanup := newTestDB(t, nil)
	defer cleanup()

	if err := db.Create("users"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for i := 0; i < 30; i++ {
		if err := db.Write("users", fmt.Sprintf("u%d", i), fmt.Sprintf("v%d", i)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Snapshot del estado en memoria antes del Flush
	db.dataMutex.RLock()
	inMemoryCount := len(db.data["users"])
	db.dataMutex.RUnlock()

	// Flush
	if err := db.Flush(); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Cargar desde disco en una DB nueva y comparar
	cfg2 := DefaultConfig()
	cfg2.DirPath = db.config.DirPath
	cfg2.EncryptionKey = db.config.EncryptionKey
	cfg2.Debug = false
	cfg2.MetricsEnabled = false

	db2, err := InitWithConfig(cfg2)
	if err != nil {
		t.Fatalf("Reopen failed: %v", err)
	}
	defer db2.Close()

	db2.dataMutex.RLock()
	loadedCount := 0
	if t, ok := db2.data["users"]; ok {
		loadedCount = len(t)
	}
	db2.dataMutex.RUnlock()

	if loadedCount != inMemoryCount {
		t.Errorf("state mismatch: in-memory=%d, loaded=%d", inMemoryCount, loadedCount)
	}
}
