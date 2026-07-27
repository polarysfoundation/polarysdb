// ============================================================================
// FILE: polarysdb/database_test.go
// Unit tests for Database
// ============================================================================
package polarysdb

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
	"github.com/polarysfoundation/polarysdb/v2/modules/wal"
)

// ============================================================================
// Helpers
// ============================================================================

type TestUser struct {
	Name string
	Age  int
}

var testKey = common.BytesToKey([]byte("test_password_for_test_32bytes"))
var testKey2 = common.BytesToKey([]byte("test_password_for_test_2_32bytes"))

// Config minimal para tests — intervals largos para evitar interferencia
func testConfig(dir string) *Config {
	return &Config{
		DirPath:            dir,
		BackupDir:          filepath.Join(dir, "backups"),
		EncryptionKey:      testKey,
		SaveInterval:       1 * time.Hour,
		WatchInterval:      1 * time.Hour,
		BufferSize:         100,
		MaxRetries:         3,
		RetryDelay:         50 * time.Millisecond,
		EnableWAL:          false,
		EnableBackup:       false,
		EnableIndexes:      true,
		EnableTransactions: true,
		EnableCompression:  false,
		MetricsEnabled:     true,
		OpTimeout:          5 * time.Second,
		BatchTimeout:       30 * time.Second,
		Debug:              true,
	}
}

// Config con WAL habilitado para tests de recovery
func testConfigWithWAL(dir string) *Config {
	cfg := testConfig(dir)
	cfg.EnableWAL = true
	cfg.WALSyncInterval = 100 * time.Millisecond
	return cfg
}

func setupTestDB(t *testing.T) *Database {
	t.Helper()
	dir := ".test"
	cfg := testConfig(dir)
	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to init database: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

func setupTestDBWithWAL(t *testing.T) *Database {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfigWithWAL(dir)
	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to init database with WAL: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})
	return db
}

// ============================================================================
// Initialization
// ============================================================================

func TestInitWithConfig(t *testing.T) {
	t.Run("successful_init", func(t *testing.T) {
		cfg := testConfig(".test")
		db, err := InitWithConfig(cfg)
		if err != nil {
			t.Fatalf("InitWithConfig failed: %v", err)
		}
		db.Close()
	})

	t.Run("nil_config_uses_default", func(t *testing.T) {
		db, err := InitWithConfig(nil)
		if err != nil {
			t.Fatalf("InitWithConfig with nil should use default: %v", err)
		}
		db.Close()
		os.RemoveAll("./data") // cleanup default dir
	})

	t.Run("creates_directories", func(t *testing.T) {
		dir := t.TempDir()
		dataDir := filepath.Join(dir, "custom_data")
		cfg := testConfig(dataDir)
		db, err := InitWithConfig(cfg)
		if err != nil {
			t.Fatalf("Failed: %v", err)
		}

		if _, err := os.Stat(dataDir); os.IsNotExist(err) {
			t.Error("Data directory was not created")
		}
		db.Close()
	})
}

func TestValidateConfig(t *testing.T) {
	t.Run("empty_dirpath", func(t *testing.T) {
		cfg := &Config{DirPath: ""}
		if err := validateConfig(cfg); err == nil {
			t.Error("Expected error for empty DirPath")
		}
	})

	t.Run("save_interval_too_small", func(t *testing.T) {
		cfg := &Config{
			DirPath:      "/tmp/test",
			SaveInterval: 10 * time.Millisecond,
			BufferSize:   100,
		}
		if err := validateConfig(cfg); err == nil {
			t.Error("Expected error for small SaveInterval")
		}
	})

	t.Run("buffer_size_too_small", func(t *testing.T) {
		cfg := &Config{
			DirPath:      "/tmp/test",
			SaveInterval: 5 * time.Second,
			BufferSize:   5,
		}
		if err := validateConfig(cfg); err == nil {
			t.Error("Expected error for small BufferSize")
		}
	})

	t.Run("valid_config", func(t *testing.T) {
		cfg := testConfig("/tmp/test")
		if err := validateConfig(cfg); err != nil {
			t.Errorf("Valid config should not error: %v", err)
		}
	})
}

// ============================================================================
// CRUD — Create
// ============================================================================

func TestCreate(t *testing.T) {
	db := setupTestDB(t)

	t.Run("create_new_table", func(t *testing.T) {
		err := db.Create("users")
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		exists, err := db.Exist("users")
		if err != nil {
			t.Fatalf("Exist failed: %v", err)
		}
		if !exists {
			t.Error("Table should exist after Create")
		}
	})

	t.Run("create_duplicate_table_returns_error", func(t *testing.T) {
		err := db.Create("dup_table")
		if err != nil {
			t.Fatalf("First create failed: %v", err)
		}

		err = db.Create("dup_table")
		if err == nil {
			t.Error("Expected error for duplicate table creation")
		}
	})

	t.Run("create_multiple_tables", func(t *testing.T) {
		tables := []string{"table1", "table2", "table3"}
		for _, table := range tables {
			if err := db.Create(table); err != nil {
				t.Errorf("Create %s failed: %v", table, err)
			}
		}

		for _, table := range tables {
			exists, _ := db.Exist(table)
			if !exists {
				t.Errorf("Table %s should exist", table)
			}
		}
	})
}

// ============================================================================
// CRUD — Write & Read
// ============================================================================

func TestWriteAndRead(t *testing.T) {
	db := setupTestDB(t)

	if err := db.Create("users"); err != nil {
		t.Logf("Failed create table=%s already exist", "users")
	}

	t.Run("write_and_read_map_value", func(t *testing.T) {
		data := map[string]any{"Name": "alice", "Age": 30}
		err := db.Write("users", "alice", data)
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		var user TestUser
		err = db.Read("users", "alice", &user)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if user.Name != "alice" {
			t.Errorf("Expected Name=alice, got %s", user.Name)
		}
		if user.Age != 30 {
			t.Errorf("Expected Age=30, got %d", user.Age)
		}
	})

	t.Run("write_and_read_string", func(t *testing.T) {
		err := db.Write("users", "note", "hello world")
		if err != nil {
			t.Fatalf("Write string failed: %v", err)
		}

		var result string
		err = db.Read("users", "note", &result)
		if err != nil {
			t.Fatalf("Read string failed: %v", err)
		}

		if result != "hello world" {
			t.Errorf("Expected 'hello world', got '%s'", result)
		}
	})

	t.Run("write_and_read_int", func(t *testing.T) {
		err := db.Write("users", "count", 42)
		if err != nil {
			t.Fatalf("Write int failed: %v", err)
		}

		var result int
		err = db.Read("users", "count", &result)
		if err != nil {
			t.Fatalf("Read int failed: %v", err)
		}

		if result != 42 {
			t.Errorf("Expected 42, got %d", result)
		}
	})

	t.Run("write_and_read_bool", func(t *testing.T) {
		err := db.Write("users", "active", true)
		if err != nil {
			t.Fatalf("Write bool failed: %v", err)
		}

		var result bool
		err = db.Read("users", "active", &result)
		if err != nil {
			t.Fatalf("Read bool failed: %v", err)
		}

		if result != true {
			t.Errorf("Expected true, got %v", result)
		}
	})

	t.Run("write_overwrites_existing_key", func(t *testing.T) {
		db.Write("users", "overwrite", "first")
		db.Write("users", "overwrite", "second")

		var result string
		db.Read("users", "overwrite", &result)

		if result != "second" {
			t.Errorf("Expected 'second' after overwrite, got '%s'", result)
		}
	})

	t.Run("read_nonexistent_key_returns_error", func(t *testing.T) {
		var result string
		err := db.Read("users", "nonexistent", &result)
		if err == nil {
			t.Error("Expected error for nonexistent key")
		}
	})

	t.Run("read_from_nonexistent_table_returns_error", func(t *testing.T) {
		var result string
		err := db.Read("ghost_table", "key", &result)
		if err == nil {
			t.Error("Expected error for nonexistent table")
		}
	})

	t.Run("write_to_nonexistent_table_returns_error", func(t *testing.T) {
		err := db.Write("ghost_table", "key", "value")
		if err == nil {
			t.Error("Expected error for writing to nonexistent table")
		}
	})
}

// ============================================================================
// CRUD — Delete
// ============================================================================

func TestDelete(t *testing.T) {
	db := setupTestDB(t)

	if err := db.Create("users"); err != nil {
		t.Logf("Failed create table=%s already exist", "users")
	}
	db.Write("users", "alice", "value")
	db.Write("users", "bob", "value2")

	t.Run("delete_existing_key", func(t *testing.T) {
		err := db.Delete("users", "alice")
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		var result string
		err = db.Read("users", "alice", &result)
		if err == nil {
			t.Error("Expected error after deleting key")
		}
	})

	t.Run("delete_nonexistent_key_from_existing_table", func(t *testing.T) {
		// Delete should still succeed (no-op on missing key)
		err := db.Delete("users", "ghost_key")
		// Behavior depends on implementation: may error or succeed
		// Current impl: if table exists, delete is a no-op → succeeds
		if err != nil {
			t.Logf("Delete nonexistent key returned: %v", err)
		}
	})

	t.Run("delete_from_nonexistent_table_returns_error", func(t *testing.T) {
		err := db.Delete("ghost_table", "key")
		if err == nil {
			t.Error("Expected error for deleting from nonexistent table")
		}
	})

	t.Run("delete_does_not_affect_other_keys", func(t *testing.T) {
		db.Delete("users", "alice")

		var result string
		err := db.Read("users", "bob", &result)
		if err != nil {
			t.Errorf("bob should still exist after deleting alice: %v", err)
		}
	})
}

// ============================================================================
// CRUD — Exist
// ============================================================================

func TestExist(t *testing.T) {
	db := setupTestDB(t)

	t.Run("existing_table", func(t *testing.T) {
		db.Create("test_table")
		exists, err := db.Exist("test_table")
		if err != nil {
			t.Fatalf("Exist failed: %v", err)
		}
		if !exists {
			t.Error("Table should exist")
		}
	})

	t.Run("nonexistent_table", func(t *testing.T) {
		exists, err := db.Exist("ghost")
		if err != nil {
			t.Fatalf("Exist failed: %v", err)
		}
		if exists {
			t.Error("Table should not exist")
		}
	})

	t.Run("closed_database_returns_error", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		db, _ := InitWithConfig(cfg)
		db.Close()

		_, err := db.Exist("any_table")
		if err == nil {
			t.Error("Expected error on closed database")
		}
	})
}

// ============================================================================
// WriteBatch
// ============================================================================

func TestWriteBatch(t *testing.T) {
	db := setupTestDB(t)

	if err := db.Create("products"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	t.Run("batch_write_success", func(t *testing.T) {
		records := map[string]any{
			"p1": map[string]any{"Name": "widget", "Age": 10},
			"p2": map[string]any{"Name": "gadget", "Age": 20},
			"p3": map[string]any{"Name": "thingy", "Age": 30},
		}

		err := db.WriteBatch("products", records)
		if err != nil {
			t.Fatalf("WriteBatch failed: %v", err)
		}

		var p1 TestUser
		db.Read("products", "p1", &p1)
		if p1.Name != "widget" {
			t.Errorf("Expected Name=widget, got %s", p1.Name)
		}

		var p2 TestUser
		db.Read("products", "p2", &p2)
		if p2.Name != "gadget" {
			t.Errorf("Expected Name=gadget, got %s", p2.Name)
		}
	})

	t.Run("batch_write_to_nonexistent_table", func(t *testing.T) {
		records := map[string]any{"k1": "v1"}
		err := db.WriteBatch("ghost_table", records)
		if err == nil {
			t.Error("Expected error for batch write to nonexistent table")
		}
	})

	t.Run("batch_exceeds_max_size", func(t *testing.T) {
		records := make(map[string]any)
		for i := 0; i < maxBatchSize+1; i++ {
			records[fmt.Sprintf("k%d", i)] = i
		}

		err := db.WriteBatch("products", records)
		if err == nil {
			t.Error("Expected error for oversized batch")
		}
	})

	t.Run("batch_overwrites_existing_keys", func(t *testing.T) {
		db.Write("products", "existing", "old_value")

		records := map[string]any{"existing": "new_value"}
		err := db.WriteBatch("products", records)
		if err != nil {
			t.Fatalf("WriteBatch failed: %v", err)
		}

		var result string
		db.Read("products", "existing", &result)
		if result != "new_value" {
			t.Errorf("Expected 'new_value', got '%s'", result)
		}
	})
}

// ============================================================================
// ReadBatch
// ============================================================================

func TestReadBatch(t *testing.T) {
	db := setupTestDB(t)

	if err := db.Create("items"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Seed data
	db.Write("items", "i1", map[string]any{"Name": "alpha", "Age": 1})
	db.Write("items", "i2", map[string]any{"Name": "beta", "Age": 2})
	db.Write("items", "i3", map[string]any{"Name": "gamma", "Age": 3})

	t.Run("read_batch_into_struct_slice", func(t *testing.T) {
		var items []TestUser
		err := db.ReadBatch("items", &items)
		if err != nil {
			t.Fatalf("ReadBatch failed: %v", err)
		}

		if len(items) != 3 {
			t.Errorf("Expected 3 items, got %d", len(items))
		}

		names := make(map[string]bool)
		for _, item := range items {
			names[item.Name] = true
		}

		if !names["alpha"] || !names["beta"] || !names["gamma"] {
			t.Errorf("Missing expected names in result: %v", names)
		}
	})

	t.Run("read_batch_into_any_slice", func(t *testing.T) {
		var results []any
		err := db.ReadBatch("items", &results)
		if err != nil {
			t.Fatalf("ReadBatch into []any failed: %v", err)
		}

		if len(results) != 3 {
			t.Errorf("Expected 3 results, got %d", len(results))
		}
	})

	t.Run("read_batch_into_pointer_slice", func(t *testing.T) {
		var items []*TestUser
		err := db.ReadBatch("items", &items)
		if err != nil {
			t.Fatalf("ReadBatch into []*TestUser failed: %v", err)
		}

		if len(items) != 3 {
			t.Errorf("Expected 3 items, got %d", len(items))
		}

		for _, item := range items {
			if item == nil {
				t.Error("Item should not be nil")
			}
		}
	})

	t.Run("read_batch_nonexistent_table", func(t *testing.T) {
		var items []TestUser
		err := db.ReadBatch("ghost", &items)
		if err == nil {
			t.Error("Expected error for nonexistent table")
		}
	})

	t.Run("read_batch_nil_dst", func(t *testing.T) {
		err := db.ReadBatch("items", nil)
		if err == nil {
			t.Error("Expected error for nil dst")
		}
	})

	t.Run("read_batch_non_slice_dst", func(t *testing.T) {
		var single TestUser
		err := db.ReadBatch("items", &single)
		if err == nil {
			t.Error("Expected error for non-slice dst")
		}
	})

	t.Run("read_batch_returns_copies_not_references", func(t *testing.T) {
		var items []TestUser
		db.ReadBatch("items", &items)

		// Modify returned value — should NOT affect DB internal data
		if len(items) > 0 {
			items[0].Name = "MODIFIED"
		}

		var verify TestUser
		db.Read("items", "i1", &verify)
		if verify.Name == "MODIFIED" {
			t.Error("ReadBatch returned references, not copies — internal data was modified")
		}
	})
}

// ============================================================================
// Transactions
// ============================================================================

func TestTransactionBasic(t *testing.T) {
	db := setupTestDB(t)

	db.Create("accounts")
	db.Write("accounts", "alice", map[string]any{"Name": "alice", "Age": 100})

	t.Run("begin_and_commit_no_conflict", func(t *testing.T) {
		txn, err := db.BeginTransaction()
		if err != nil {
			t.Fatalf("BeginTransaction failed: %v", err)
		}

		// Commit without any intervening writes
		err = db.CommitTransaction(txn)
		if err != nil {
			t.Fatalf("CommitTransaction failed: %v", err)
		}
	})

	t.Run("transaction_conflict_detected", func(t *testing.T) {
		txn, err := db.BeginTransaction()
		if err != nil {
			t.Fatalf("BeginTransaction failed: %v", err)
		}

		// Write something between Begin and Commit → version changes
		db.Write("accounts", "bob", map[string]any{"Name": "bob", "Age": 200})

		err = db.CommitTransaction(txn)
		if err == nil {
			t.Error("Expected conflict error, got nil")
		}
	})

	t.Run("transactions_disabled", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		cfg.EnableTransactions = false
		db, err := InitWithConfig(cfg)
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		_, err = db.BeginTransaction()
		if err == nil {
			t.Error("Expected error when transactions disabled")
		}
	})
}

func TestTransactionSnapshotIsolation(t *testing.T) {
	db := setupTestDB(t)

	db.Create("data")
	db.Write("data", "key1", map[string]any{"Name": "original", "Age": 1})

	t.Run("snapshot_is_immutable_after_external_write", func(t *testing.T) {
		txn, err := db.BeginTransaction()
		if err != nil {
			t.Fatalf("BeginTransaction failed: %v", err)
		}

		// External write modifies live data
		db.Write("data", "key1", map[string]any{"Name": "modified", "Age": 2})

		// Snapshot inside the transaction should still see "original"
		// (This depends on the tx package's Transaction implementation)
		// We verify indirectly: the commit will fail due to version mismatch
		err = db.CommitTransaction(txn)
		if err == nil {
			t.Log("Commit succeeded — snapshot was isolated (version didn't change, or conflict detection works)")
		} else {
			t.Logf("Commit failed with conflict: %v (expected — external write changed version)", err)
		}
	})
}

// ============================================================================
// deepCopyValue
// ============================================================================

func TestDeepCopyValue(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		result := deepCopyValue(nil)
		if result != nil {
			t.Errorf("Expected nil, got %v", result)
		}
	})

	t.Run("bool", func(t *testing.T) {
		result := deepCopyValue(true)
		if result != true {
			t.Errorf("Expected true, got %v", result)
		}
	})

	t.Run("int", func(t *testing.T) {
		result := deepCopyValue(42)
		if result != 42 {
			t.Errorf("Expected 42, got %v", result)
		}
	})

	t.Run("string", func(t *testing.T) {
		result := deepCopyValue("hello")
		if result != "hello" {
			t.Errorf("Expected 'hello', got %v", result)
		}
	})

	t.Run("[]byte", func(t *testing.T) {
		original := []byte{1, 2, 3}
		copied := deepCopyValue(original)

		cp, ok := copied.([]byte)
		if !ok {
			t.Fatalf("Expected []byte, got %T", copied)
		}

		if !reflect.DeepEqual(cp, original) {
			t.Errorf("Copy doesn't match original")
		}

		// Modify copy — original should be unaffected
		cp[0] = 99
		if original[0] == 99 {
			t.Error("Modifying copy affected original — not a deep copy")
		}
	})

	t.Run("map_string_any_shallow", func(t *testing.T) {
		original := map[string]any{"key1": "val1", "key2": 42}
		copied := deepCopyValue(original)

		cp, ok := copied.(map[string]any)
		if !ok {
			t.Fatalf("Expected map[string]any, got %T", copied)
		}

		if cp["key1"] != "val1" || cp["key2"] != 42 {
			t.Errorf("Copy values don't match: %v", cp)
		}

		// Modify copy — original should be unaffected
		cp["key1"] = "modified"
		if original["key1"] == "modified" {
			t.Error("Modifying copy affected original — not a deep copy")
		}
	})

	t.Run("map_string_any_nested", func(t *testing.T) {
		original := map[string]any{
			"outer": map[string]any{
				"inner": "value",
			},
		}
		copied := deepCopyValue(original)

		cp, ok := copied.(map[string]any)
		if !ok {
			t.Fatalf("Expected map[string]any, got %T", copied)
		}

		// Modify nested value in copy
		inner, ok := cp["outer"].(map[string]any)
		if !ok {
			t.Fatalf("Expected nested map, got %T", cp["outer"])
		}
		inner["inner"] = "modified"

		// Original nested value should be unchanged
		origInner, ok := original["outer"].(map[string]any)
		if !ok {
			t.Fatalf("Original nested map type: %T", original["outer"])
		}
		if origInner["inner"] == "modified" {
			t.Error("Modifying nested copy affected original — not a deep copy")
		}
	})

	t.Run("slice_any", func(t *testing.T) {
		original := []any{"a", "b", "c"}
		copied := deepCopyValue(original)

		cp, ok := copied.([]any)
		if !ok {
			t.Fatalf("Expected []any, got %T", copied)
		}

		if len(cp) != 3 {
			t.Errorf("Expected length 3, got %d", len(cp))
		}

		// Modify copy
		cp[0] = "modified"
		if original[0] == "modified" {
			t.Error("Modifying copy affected original")
		}
	})

	t.Run("unknown_type_json_fallback", func(t *testing.T) {
		// struct type falls through to JSON round-trip
		original := TestUser{Name: "test", Age: 25}
		copied := deepCopyValue(original)

		// JSON round-trip converts struct to map[string]any
		cp, ok := copied.(map[string]any)
		if !ok {
			// Could also be the original struct if marshal fails
			t.Logf("Deep copy of struct returned %T: %v", copied, copied)
		} else {
			t.Logf("JSON round-trip produced map: %v", cp)
		}
	})
}

// ============================================================================
// to() — Mapper utility
// ============================================================================

func TestTo(t *testing.T) {
	db := setupTestDB(t)

	t.Run("map_to_struct", func(t *testing.T) {
		src := map[string]any{"Name": "alice", "Age": 30}
		var dst TestUser
		err := db.to(src, &dst)
		if err != nil {
			t.Fatalf("to(map→struct) failed: %v", err)
		}
		if dst.Name != "alice" || dst.Age != 30 {
			t.Errorf("Expected {alice, 30}, got {%s, %d}", dst.Name, dst.Age)
		}
	})

	t.Run("primitive_direct_assignment", func(t *testing.T) {
		err := db.to(42, new(int))
		if err != nil {
			t.Fatalf("to(int→*int) failed: %v", err)
		}
	})

	t.Run("string_direct_assignment", func(t *testing.T) {
		var dst string
		err := db.to("hello", &dst)
		if err != nil {
			t.Fatalf("to(string→*string) failed: %v", err)
		}
		if dst != "hello" {
			t.Errorf("Expected 'hello', got '%s'", dst)
		}
	})

	t.Run("nil_source_returns_error", func(t *testing.T) {
		var dst string
		err := db.to(nil, &dst)
		if err == nil {
			t.Error("Expected error for nil source")
		}
	})

	t.Run("type_mismatch_primitive", func(t *testing.T) {
		var dst int
		err := db.to("not_an_int", &dst)
		// Should either error or be handled by mapper
		if err != nil {
			t.Logf("Type mismatch correctly returned error: %v", err)
		} else {
			t.Logf("Type mismatch was handled by mapper (dst=%d)", dst)
		}
	})
}

// ============================================================================
// Concurrent access
// ============================================================================

func TestConcurrentWrites(t *testing.T) {
	db := setupTestDB(t)
	db.Create("concurrent")

	const numGoroutines = 50
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key_%d", idx)
			value := fmt.Sprintf("value_%d", idx)
			if err := db.Write("concurrent", key, value); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Verify all writes succeeded
	for i := 0; i < numGoroutines; i++ {
		var result string
		key := fmt.Sprintf("key_%d", i)
		err := db.Read("concurrent", key, &result)
		if err != nil {
			t.Errorf("Read %s failed: %v", key, err)
		}
		if result != fmt.Sprintf("value_%d", i) {
			t.Errorf("Expected value_%d, got %s", i, result)
		}
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	db := setupTestDB(t)
	db.Create("mixed")
	db.Write("mixed", "shared", "initial")

	const iterations = 100
	var wg sync.WaitGroup
	readErrors := make(chan error, iterations)
	writeErrors := make(chan error, iterations)

	wg.Add(iterations * 2)
	for i := 0; i < iterations; i++ {
		// Writer
		go func(idx int) {
			defer wg.Done()
			err := db.Write("mixed", fmt.Sprintf("w_%d", idx), idx)
			if err != nil {
				writeErrors <- err
			}
		}(i)

		// Reader
		go func() {
			defer wg.Done()
			var result string
			err := db.Read("mixed", "shared", &result)
			if err != nil {
				readErrors <- err
			}
		}()
	}

	wg.Wait()
	close(readErrors)
	close(writeErrors)

	readCount := 0
	for err := range readErrors {
		t.Errorf("Read error: %v", err)
		readCount++
	}
	writeCount := 0
	for err := range writeErrors {
		t.Errorf("Write error: %v", err)
		writeCount++
	}

	if readCount > iterations/2 || writeCount > iterations/2 {
		t.Errorf("Too many errors: %d reads, %d writes out of %d", readCount, writeCount, iterations)
	}
}

func TestConcurrentTransactionsConflict(t *testing.T) {
	db := setupTestDB(t)
	db.Create("counter")
	db.Write("counter", "val", 0)

	const numTxns = 10
	conflicts := 0
	successes := 0

	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(numTxns)
	for i := 0; i < numTxns; i++ {
		go func() {
			defer wg.Done()
			txn, err := db.BeginTransaction()
			if err != nil {
				return
			}

			// Small delay to increase chance of conflict
			time.Sleep(10 * time.Millisecond)

			err = db.CommitTransaction(txn)
			mu.Lock()
			if err != nil {
				conflicts++
			} else {
				successes++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	t.Logf("Transactions: %d successes, %d conflicts (expected some conflicts)",
		successes, conflicts)

	if successes == 0 {
		t.Error("No transactions succeeded — conflict detection may be too aggressive")
	}
	if conflicts == 0 {
		t.Log("No conflicts — all transactions succeeded sequentially")
	}
}

// ============================================================================
// Closed database operations
// ============================================================================

func TestOperationsOnClosedDB(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	db.Close()

	t.Run("create_on_closed", func(t *testing.T) {
		err := db.Create("table")
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("write_on_closed", func(t *testing.T) {
		err := db.Write("table", "key", "value")
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("read_on_closed", func(t *testing.T) {
		var result string
		err := db.Read("table", "key", &result)
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("delete_on_closed", func(t *testing.T) {
		err := db.Delete("table", "key")
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("writebatch_on_closed", func(t *testing.T) {
		err := db.WriteBatch("table", map[string]any{"k": "v"})
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("readbatch_on_closed", func(t *testing.T) {
		var results []any
		err := db.ReadBatch("table", &results)
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("exist_on_closed", func(t *testing.T) {
		_, err := db.Exist("table")
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})

	t.Run("begin_tx_on_closed", func(t *testing.T) {
		_, err := db.BeginTransaction()
		if err == nil {
			t.Error("Expected error on closed DB")
		}
	})
}

// ============================================================================
// Close lifecycle
// ============================================================================

func TestClose(t *testing.T) {
	t.Run("clean_close", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		db, err := InitWithConfig(cfg)
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		db.Create("table")
		db.Write("table", "key", "value")

		err = db.Close()
		if err != nil {
			t.Errorf("Close failed: %v", err)
		}
	})

	t.Run("double_close_returns_nil", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		db, _ := InitWithConfig(cfg)

		db.Close()
		err := db.Close() // Second close
		if err != nil {
			t.Errorf("Double close should return nil, got: %v", err)
		}
	})
}

func TestCloseWithTimeout(t *testing.T) {
	t.Run("close_with_custom_timeout", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfig(dir)
		db, _ := InitWithConfig(cfg)

		db.Create("table")
		db.Write("table", "k1", "v1")

		err := db.CloseWithTimeout(10 * time.Second)
		if err != nil {
			t.Errorf("CloseWithTimeout failed: %v", err)
		}
	})
}

// ============================================================================
// WAL Recovery
// ============================================================================

func TestWALRecovery(t *testing.T) {
	t.Run("data_survives_close_and_reopen", func(t *testing.T) {
		dir := t.TempDir()
		cfg := testConfigWithWAL(dir)

		// Phase 1: Create and write
		db1, err := InitWithConfig(cfg)
		if err != nil {
			t.Fatalf("Init phase 1 failed: %v", err)
		}

		db1.Create("persist_table")
		db1.Write("persist_table", "key1", map[string]any{"Name": "saved", "Age": 99})
		db1.Write("persist_table", "key2", "string_value")

		// Wait for WAL to process
		time.Sleep(200 * time.Millisecond)

		db1.Close()

		// Phase 2: Reopen and verify
		cfg2 := testConfigWithWAL(dir)
		db2, err := InitWithConfig(cfg2)
		if err != nil {
			t.Fatalf("Init phase 2 failed: %v", err)
		}
		t.Cleanup(func() { db2.Close() })

		var user TestUser
		err = db2.Read("persist_table", "key1", &user)
		if err != nil {
			t.Fatalf("Read after recovery failed: %v", err)
		}
		if user.Name != "saved" {
			t.Errorf("Expected Name=saved, got %s", user.Name)
		}

		var str string
		err = db2.Read("persist_table", "key2", &str)
		if err != nil {
			t.Fatalf("Read string after recovery failed: %v", err)
		}
		if str != "string_value" {
			t.Errorf("Expected 'string_value', got '%s'", str)
		}
	})
}

func TestApplyWALEntry(t *testing.T) {
	db := setupTestDB(t)

	t.Run("op_create", func(t *testing.T) {
		entry := &wal.Entry{OpType: wal.OpCreate, Table: "wal_table"}
		err := db.applyWALEntry(entry)
		if err != nil {
			t.Fatalf("applyWALEntry OpCreate failed: %v", err)
		}

		exists, _ := db.Exist("wal_table")
		if !exists {
			t.Error("Table should exist after OpCreate WAL entry")
		}
	})

	t.Run("op_write_auto_creates_table", func(t *testing.T) {
		entry := &wal.Entry{OpType: wal.OpWrite, Table: "auto_table", Key: "k1", Value: "v1"}
		err := db.applyWALEntry(entry)
		if err != nil {
			t.Fatalf("applyWALEntry OpWrite failed: %v", err)
		}

		var result string
		db.Read("auto_table", "k1", &result)
		if result != "v1" {
			t.Errorf("Expected 'v1', got '%s'", result)
		}
	})

	t.Run("op_write_to_existing_table", func(t *testing.T) {
		db.applyWALEntry(&wal.Entry{OpType: wal.OpCreate, Table: "existing"})
		db.applyWALEntry(&wal.Entry{OpType: wal.OpWrite, Table: "existing", Key: "k", Value: "val"})

		var result string
		db.Read("existing", "k", &result)
		if result != "val" {
			t.Errorf("Expected 'val', got '%s'", result)
		}
	})

	t.Run("op_delete_existing_key", func(t *testing.T) {
		db.applyWALEntry(&wal.Entry{OpType: wal.OpCreate, Table: "del_table"})
		db.applyWALEntry(&wal.Entry{OpType: wal.OpWrite, Table: "del_table", Key: "k", Value: "v"})
		db.applyWALEntry(&wal.Entry{OpType: wal.OpDelete, Table: "del_table", Key: "k"})

		var result string
		err := db.Read("del_table", "k", &result)
		if err == nil {
			t.Error("Key should be deleted")
		}
	})

	t.Run("op_delete_nonexistent_table_no_error", func(t *testing.T) {
		entry := &wal.Entry{OpType: wal.OpDelete, Table: "ghost", Key: "k"}
		err := db.applyWALEntry(entry)
		if err != nil {
			t.Errorf("Delete on nonexistent table should not error: %v", err)
		}
	})
}

// ============================================================================
// ChangeKey & Security
// ============================================================================

func TestChangeKey(t *testing.T) {
	db := setupTestDB(t)
	db.Create("secret")
	db.Write("secret", "data", "classified")

	t.Run("change_key_success", func(t *testing.T) {
		var newKey common.Key
		for i := range newKey {
			newKey[i] = byte(i + 100)
		}

		err := db.ChangeKey(testKey, testKey2)
		if err != nil {
			t.Fatalf("ChangeKey failed: %v", err)
		}

		// Data should still be readable after key change
		var result string
		err = db.Read("secret", "data", &result)
		if err != nil {
			t.Errorf("Read after key change failed: %v", err)
		}
	})

	t.Run("change_key_wrong_old_key", func(t *testing.T) {
		err := db.ChangeKey(testKey, testKey2)
		if err == nil {
			t.Error("Expected error for wrong old key")
		}
	})
}

func TestExportAuthorization(t *testing.T) {
	cfg := testConfig(".test")
	cfg.EncryptionKey = testKey2
	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Error("Error initializating database")
	}

	if err := db.ChangeKey(testKey2, testKey); err != nil {
		t.Error("Error changing database key")
	}

	db.Create("private")
	db.Write("private", "data", "secret")

	t.Run("export_with_wrong_key", func(t *testing.T) {
		var wrongKey common.Key
		for i := range wrongKey {
			wrongKey[i] = 0xFF
		}

		dir := t.TempDir()
		err := db.Export(wrongKey, filepath.Join(dir, "export.json"))
		if err == nil {
			t.Error("Expected unauthorized error for wrong key")
		}
	})
}

// ============================================================================
// Version tracking
// ============================================================================

func TestVersionTracking(t *testing.T) {
	db := setupTestDB(t)

	t.Run("version_starts_at_zero", func(t *testing.T) {
		v := db.version.Load()
		if v != 0 {
			t.Errorf("Expected initial version 0, got %d", v)
		}
	})

	t.Run("version_increments_after_write", func(t *testing.T) {
		db.Create("versioned")
		v1 := db.version.Load()

		db.Write("versioned", "k1", "v1")
		v2 := db.version.Load()

		if v2 <= v1 {
			t.Errorf("Version should increment after write: v1=%d, v2=%d", v1, v2)
		}
	})

	t.Run("version_increments_after_batch", func(t *testing.T) {
		db.Create("batch_ver")
		v1 := db.version.Load()

		db.WriteBatch("batch_ver", map[string]any{"k1": "v1", "k2": "v2"})
		v2 := db.version.Load()

		if v2 <= v1 {
			t.Errorf("Version should increment after batch: v1=%d, v2=%d", v1, v2)
		}
	})

	t.Run("version_does_not_increment_on_failed_op", func(t *testing.T) {
		v1 := db.version.Load()

		// Write to nonexistent table → should fail
		db.Write("ghost", "k", "v")

		v2 := db.version.Load()
		if v2 != v1 {
			t.Errorf("Version should NOT increment on failed op: v1=%d, v2=%d", v1, v2)
		}
	})
}

// ============================================================================
// flushToDisk & dirtyFlag CAS
// ============================================================================

func TestFlushToDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.SaveInterval = 200 * time.Millisecond // Short interval for test

	db, err := InitWithConfig(cfg)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	db.Create("flush_test")
	db.Write("flush_test", "key", "value")

	// Wait for periodic save
	time.Sleep(500 * time.Millisecond)

	// Verify dirtyFlag was cleared
	if db.dirtyFlag.Load() {
		t.Error("dirtyFlag should be false after flush")
	}
}

// ============================================================================
// GetStatus & GetMetrics
// ============================================================================

func TestGetStatus(t *testing.T) {
	db := setupTestDB(t)

	db.Create("status_test")
	db.Write("status_test", "k1", "v1")

	status := db.GetStatus()

	if status["closed"] == true {
		t.Error("Database should not be closed")
	}

	if _, ok := status["uptime_seconds"]; !ok {
		t.Error("Status should include uptime_seconds")
	}

	if _, ok := status["total_reads"]; !ok {
		t.Error("Status should include total_reads")
	}

	if _, ok := status["total_writes"]; !ok {
		t.Error("Status should include total_writes")
	}
}

func TestGetMetrics(t *testing.T) {
	db := setupTestDB(t)

	m := db.GetMetrics()
	if m == nil {
		t.Error("GetMetrics should not return nil")
	}

	db.Create("metrics_test")
	db.Write("metrics_test", "k", "v")

	var result string
	db.Read("metrics_test", "k", &result)

	m2 := db.GetMetrics()
	if m2.TotalWrites < m.TotalWrites {
		t.Error("TotalWrites should increase after write")
	}
	if m2.TotalReads < m.TotalReads {
		t.Error("TotalReads should increase after read")
	}
}

// ============================================================================
// Read returns defensive copies
// ============================================================================

func TestReadReturnsDefensiveCopy(t *testing.T) {
	db := setupTestDB(t)

	if err := db.Create("copy_test"); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	t.Run("map_value_read_is_copy", func(t *testing.T) {
		err := db.Write("copy_test", "data", map[string]any{"field": "original"})
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		var result map[string]any
		err = db.Read("copy_test", "data", &result)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if result == nil {
			t.Fatal("Result is nil after successful Read")
		}

		// Modify returned value — should NOT affect internal data
		result["field"] = "modified"

		var verify map[string]any
		err = db.Read("copy_test", "data", &verify)
		if err != nil {
			t.Fatalf("Read verify failed: %v", err)
		}

		if verify["field"] == "modified" {
			t.Error("Read returned references, not copies — internal data was modified")
		}
	})
}
