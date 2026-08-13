package wal_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
	"github.com/polarysfoundation/polarysdb/v2/modules/logger"
	"github.com/polarysfoundation/polarysdb/v2/modules/wal"
)

// Helper para generar una clave AES-256 válida (32 bytes)
func generateTestKey(t *testing.T) common.Key {
	t.Helper()
	// Asumiendo que common.Key acepta o se construye desde un []byte de 32 bytes
	rawKey := []byte("12345678901234567890123456789012") // 32 bytes
	return common.Key(rawKey)                            // Ajusta según el tipo exacto de common.Key en tu codebase
}

func setupTestLogger() *logger.Logger {
	logCfg := logger.Config{
		MinLevel:  logger.LevelInfo,
		ToConsole: true,
		ToFile:    false,
	}

	return logger.NewLogger(logCfg) // O tu constructor de logger por defecto para tests
}

func TestWAL_Encryption_WriteAndRead(t *testing.T) {
	tempDir := t.TempDir()
	walPath := filepath.Join(tempDir, "encrypted.wal")
	key := generateTestKey(t)

	cfg := &wal.Config{
		Path:         walPath,
		SyncInterval: 10 * time.Millisecond,
		MaxSize:      10 * 1024 * 1024,
		GroupCommit:  true,
		BatchSize:    1,
		Key:          key,
	}

	log := setupTestLogger()
	w, err := wal.New(cfg, log)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.SetContext(ctx)
	w.Start()

	// 1. Escribir entradas de prueba
	entriesToWrite := []*wal.Entry{
		{
			OpType:    wal.OpWrite,
			Table:     "users",
			Key:       "user_101",
			Value:     "Gisselle Villanueva",
			TxID:      "tx_001",
			Timestamp: time.Now().UnixNano(),
		},
		{
			OpType:    wal.OpDelete,
			Table:     "users",
			Key:       "user_102",
			Value:     nil,
			TxID:      "tx_002",
			Timestamp: time.Now().UnixNano(),
		},
	}

	for _, entry := range entriesToWrite {
		if err := w.Append(entry); err != nil {
			t.Fatalf("failed to append entry: %v", err)
		}
	}

	// Cerrar WAL para asegurar que se flusheen todos los bytes a disco
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close WAL: %v", err)
	}

	// 2. Verificar que los datos en el archivo FÍSICO NO estén en texto plano
	fileBytes, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("failed to read raw WAL file: %v", err)
	}

	sensitiveData := []byte("Gisselle Villanueva")
	if bytes.Contains(fileBytes, sensitiveData) {
		t.Fatalf("SECURITY FAILURE: Sensitive data found unencrypted in WAL file!")
	}

	// 3. Reabrir el WAL con la clave correcta y verificar lectura
	wRead, err := wal.New(cfg, log)
	if err != nil {
		t.Fatalf("failed to reopen WAL: %v", err)
	}

	readEntries, err := wRead.ReadAll()
	if err != nil {
		t.Fatalf("failed to read entries from encrypted WAL: %v", err)
	}

	if len(readEntries) != len(entriesToWrite) {
		t.Fatalf("expected %d entries, got %d", len(entriesToWrite), len(readEntries))
	}

	// Validar contenido recuperado
	if readEntries[0].Key != "user_101" || readEntries[0].Value != "Gisselle Villanueva" {
		t.Errorf("mismatch in entry 0: %+v", readEntries[0])
	}
	if readEntries[1].Key != "user_102" || readEntries[1].OpType != wal.OpDelete {
		t.Errorf("mismatch in entry 1: %+v", readEntries[1])
	}
}

func TestWAL_Encryption_WrongKeyFails(t *testing.T) {
	tempDir := t.TempDir()
	walPath := filepath.Join(tempDir, "encrypted_wrong_key.wal")
	correctKey := generateTestKey(t)
	wrongKey := common.Key([]byte("wrongkey123456789012345678901234"))

	cfg := &wal.Config{
		Path:        walPath,
		BatchSize:   1,
		GroupCommit: true,
		Key:         correctKey,
	}

	log := setupTestLogger()
	w, err := wal.New(cfg, log)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.SetContext(ctx)
	w.Start()

	// Escribir entrada
	err = w.Append(&wal.Entry{
		OpType: wal.OpWrite,
		Table:  "accounts",
		Key:    "acc_99",
		Value:  "secret_balance",
	})
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	w.Close()

	// Intentar leer con una clave INCORRECTA
	wrongCfg := *cfg
	wrongCfg.Key = wrongKey

	wWrongKey, err := wal.New(&wrongCfg, log)
	if err != nil {
		t.Fatalf("failed to open WAL with wrong key config: %v", err)
	}

	entries, err := wWrongKey.ReadAll()
	// ReadAll ignora o retorna corrupciones según tu implementación (las loguea como Warnf)
	// por lo tanto no debería retornar las entradas descifradas correctamente.
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries when reading with wrong key, got %d", len(entries))
	}
}

func TestWAL_Unencrypted_To_Encrypted_Compatibility(t *testing.T) {
	tempDir := t.TempDir()
	walPath := filepath.Join(tempDir, "unencrypted.wal")

	// Configuración SIN clave
	cfgUnencrypted := &wal.Config{
		Path:        walPath,
		BatchSize:   1,
		GroupCommit: true,
		Key:         common.Key{},
	}

	log := setupTestLogger()

	w, err := wal.New(cfgUnencrypted, log)
	if err != nil {
		t.Fatalf("failed to create unencrypted WAL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.SetContext(ctx)
	w.Start()

	_ = w.Append(&wal.Entry{
		OpType: wal.OpWrite,
		Table:  "plain",
		Key:    "k1",
		Value:  "plain_value",
	})
	w.Close()

	// 1. Validar que en modo sin cifrar SÍ esté el texto plano en disco
	fileBytes, _ := os.ReadFile(walPath)
	if !bytes.Contains(fileBytes, []byte("plain_value")) {
		t.Fatalf("expected raw text in unencrypted WAL file")
	}

	// 2. Leer con modo sin cifrar debe funcionar
	wRead, _ := wal.New(cfgUnencrypted, log)
	entries, err := wRead.ReadAll()
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed to read unencrypted WAL: %v", err)
	}
}
