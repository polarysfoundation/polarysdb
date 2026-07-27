# 🗄️ PolarysDB

[![Version](https://img.shields.io/badge/version-2.0.0-blue.svg)](https://github.com/polarysfoundation/polarysdb/releases)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Protocol Buffers](https://img.shields.io/badge/Protocol_Buffers-3.0-4285F4?style=flat)](https://protobuf.dev/)

> **Enterprise-grade embedded database for Go with encryption, ACID transactions, and binary WAL.**

PolarysDB is a high-performance, embedded database designed for Go applications that need reliability, security, and speed without the complexity of external database servers.

## ⚠️ v2.0.0 Breaking Changes

If upgrading from v1.x, see the [Migration Guide](#-migration-guide-v1--v2) below. Key changes:

- `Read(table, key) (any, bool)` → `Read(table, key, dst any) error`
- `ReadBatch(table) ([]any, error)` → `ReadBatch(table, dst any) error`
- `Exist(table) bool` → `Exist(table) (bool, error)`
- `Create(table)` now returns error on duplicate table

## ✨ Key Features

### 🚀 Performance
- **50,000+ operations/second** with binary WAL
- **Asynchronous writes** with automatic batching
- **Group Commit** for maximum throughput
- **In-memory indexes** for O(1) lookups
- **10x faster** than JSON-based alternatives

### 🔒 Security
- **AES-256 encryption** at rest
- **CRC32 checksums** for data integrity
- **File locking** to prevent corruption
- **Key rotation** without downtime
- **Secure by default**

### 💪 Reliability
- **Write-Ahead Log (WAL)** with Protocol Buffers
- **Automatic recovery** from WAL on startup
- **ACID transactions** with snapshots and conflict detection
- **Automatic backups** with rotation
- **Zero data loss** guarantee

### 🎯 Developer Experience
- **Type-safe reads** with automatic mapper
- **Defensive copies** — modifying read values never corrupts internal state
- **Zero dependencies** (except Go stdlib + protobuf)
- **Embedded database** (no server required)
- **Flexible configuration**

## 📊 Benchmarks

Real-world performance on MacBook Pro M1, 16GB RAM:

| Operation | Throughput | Latency | Notes |
|-----------|------------|---------|-------|
| Single Write | 50,000 ops/s | 0.5-1ms | With WAL |
| Single Read | 500,000 ops/s | 0.05ms | Memory hit |
| Batch Write (100) | 100,000 ops/s | 10ms | Batch of 100 |
| Concurrent (100 workers) | 80,000 ops/s | 1.2ms | 100 goroutines |
| Index Query | 200,000 ops/s | 0.1ms | Hash index |
| Transaction Commit | 10,000 ops/s | 5ms | With sync |

### Comparison with Other Systems

| Database | Write (ops/s) | Read (ops/s) | Features |
|----------|---------------|--------------|----------|
| **PolarysDB** | **50,000** | **500,000** | Embedded, encrypted, WAL |
| SQLite | 35,000 | 400,000 | Embedded, ACID |
| BoltDB | 30,000 | 350,000 | Embedded, B+ Tree |
| BadgerDB | 60,000 | 450,000 | Embedded, LSM-Tree |

## 🚀 Quick Start

### Prerequisites

```bash
# Go 1.19 or higher
go version

# Protocol Buffers compiler
# macOS
brew install protobuf

# Linux
sudo apt-get install protobuf-compiler

# Windows
choco install protoc
```

### Installation

```bash
go get github.com/polarysfoundation/polarysdb@v2.0.0
```

### Basic Usage

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/polarysfoundation/polarysdb"
    "github.com/polarysfoundation/polarysdb/modules/common"
)

func main() {
    // Create encryption key (32 bytes)
    var key common.Key
    copy(key[:], []byte("my-secret-encryption-key-32b"))
    
    // Initialize database
    db, err := polarysdb.Init(key, "./data", false)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
    
    // Create table
    if err := db.Create("users"); err != nil {
        log.Fatal(err)
    }
    
    // Write data
    err = db.Write("users", "alice", map[string]any{
        "name":  "Alice",
        "email": "alice@example.com",
        "age":   30,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Read into typed struct (mapper auto-maps map[string]any → struct)
    type User struct { Name string; Email string; Age int }
    var user User
    err = db.Read("users", "alice", &user)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("User: %+v\n", user)
    
    // Read into primitive
    var count int
    db.Write("config", "count", 42)
    db.Read("config", "count", &count)
    fmt.Printf("Count: %d\n", count)
    
    // Read into map (deep copy — modifying result won't corrupt DB)
    var raw map[string]any
    db.Read("users", "alice", &raw)
    raw["age"] = 99  // ✅ Safe — only modifies your copy
    
    // Delete data
    db.Delete("users", "alice")
}
```

## 📚 Advanced Usage

### Custom Configuration

```go
cfg := polarysdb.DefaultConfig()
cfg.DirPath = "./data"
cfg.BackupDir = "./backups"
cfg.EncryptionKey = key
cfg.EnableWAL = true
cfg.EnableBackup = true
cfg.EnableIndexes = true
cfg.EnableTransactions = true
cfg.SaveInterval = 10 * time.Second
cfg.BufferSize = 2000
cfg.OpTimeout = 5 * time.Second
cfg.BatchTimeout = 30 * time.Second
cfg.Debug = true

db, err := polarysdb.InitWithConfig(cfg)
```

### Read with Mapper

`Read` automatically maps stored values into the destination type:

```go
// Map → Struct (most common)
type Product struct {
    Name     string
    Price    float64
    Category string
}
var product Product
db.Read("products", "item1", &product)

// Primitive → Primitive
var name string
db.Read("config", "username", &name)

var active bool
db.Read("config", "active", &active)

// Map → Map (deep copy, modifying won't affect internal state)
var data map[string]any
db.Read("products", "item1", &data)
data["price"] = 0 // ✅ Safe — your copy only
```

### ReadBatch with Mapper

`ReadBatch` reads all values from a table into a typed slice:

```go
// Into struct slice
var users []User
err := db.ReadBatch("users", &users)

// Into pointer slice
var users []*User
err := db.ReadBatch("users", &users)

// Into raw slice (deep copies)
var raw []any
err := db.ReadBatch("users", &raw)
```

### ACID Transactions

Transactions use optimistic concurrency — if data changes between 
`BeginTransaction` and `CommitTransaction`, the commit fails with a 
conflict error. Retry the transaction.

```go
// Begin transaction (creates deep-copy snapshot)
tx, err := db.BeginTransaction()
if err != nil {
    log.Fatal(err)
}

// Perform operations on snapshot
tx.Write("accounts", "alice", map[string]any{"balance": 900})
tx.Write("accounts", "bob", map[string]any{"balance": 600})

// Commit with conflict detection
if err := db.CommitTransaction(tx); err != nil {
    if strings.Contains(err.Error(), "transaction conflict") {
        // Retry the transaction
        log.Println("Conflict detected, retrying...")
        // ... begin new transaction and retry
    }
    tx.Rollback()
    log.Fatal(err)
}
```

### Fast Lookups with Indexes

```go
// Create index
db.CreateIndex("products", "category")

// Query by index (O(1) performance)
results, err := db.QueryByIndex("products", "category", "Electronics")
if err != nil {
    log.Fatal(err)
}

for _, product := range results {
    fmt.Printf("Product: %+v\n", product)
}
```

### Batch Operations

```go
// Prepare batch of 1000 records
batch := make(map[string]any)
for i := 0; i < 1000; i++ {
    key := fmt.Sprintf("log%d", i)
    batch[key] = map[string]any{
        "timestamp": time.Now().Unix(),
        "message":   fmt.Sprintf("Log message %d", i),
    }
}

// Write batch (10x faster than individual writes)
if err := db.WriteBatch("logs", batch); err != nil {
    log.Fatal(err)
}
```

### Backup and Restore

```go
// Export (plain JSON)
err := db.Export(key, "./backup.json")

// Export encrypted
err := db.ExportEncrypted(key, "./backup.db")

// Import
err := db.Import(key, "./backup.json")

// Import encrypted
err := db.ImportEncrypted(key, "./backup.db")
```

### Key Rotation

```go
// Change encryption key (atomic — save with new key first, rollback on failure)
var newKey common.Key
copy(newKey[:], []byte("new-secret-encryption-key-32b"))

err := db.ChangeKey(oldKey, newKey)
if err != nil {
    log.Fatal(err)
}
```

### Monitoring and Metrics

```go
// Get metrics
metrics := db.GetMetrics()
fmt.Printf("Total Reads: %d\n", metrics.TotalReads)
fmt.Printf("Total Writes: %d\n", metrics.TotalWrites)
fmt.Printf("Avg Read Latency: %v\n", metrics.AvgReadLatency)
fmt.Printf("Avg Write Latency: %v\n", metrics.AvgWriteLatency)

// Get system status
status := db.GetStatus()
fmt.Printf("Status: %+v\n", status)
```

## 🔄 Migration Guide (v1 → v2)

### Read

```go
// v1 — returns any, caller must type-assert
value, ok := db.Read("users", "alice")
if !ok { /* not found */ }
name := value.(map[string]any)["Name"].(string)

// v2 — maps into destination, returns error
var user User
err := db.Read("users", "alice", &user)
if err != nil { /* not found, table missing, or mapping error */ }
name := user.Name

// v2 — still works with raw maps (deep copy)
var raw map[string]any
err := db.Read("users", "alice", &raw)
if err != nil { return err }
raw["age"] = 99  // Safe — only modifies your copy
```

### ReadBatch

```go
// v1 — returns []any, caller must type-assert each element
values, err := db.ReadBatch("users")
for _, v := range values {
    name := v.(map[string]any)["Name"].(string)
}

// v2 — maps into typed slice
var users []User
err := db.ReadBatch("users", &users)
for _, u := range users {
    name := u.Name
}

// v2 — raw values still available
var raw []any
err := db.ReadBatch("users", &raw)
```

### Exist

```go
// v1 — returns bool only
if db.Exist("users") {
    // table exists... or DB is closed? Can't tell.
}

// v2 — returns (bool, error)
exists, err := db.Exist("users")
if err != nil {
    return err  // database is closed
}
if exists {
    // table genuinely exists
}
```

### Create

```go
// v1 — silent on duplicate
db.Create("users")  // no error if table already exists

// v2 — error on duplicate
err := db.Create("users")
if err != nil && strings.Contains(err.Error(), "already exists") {
    // Table already exists, that's OK — continue
} else if err != nil {
    return err  // real error
}
```

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     PUBLIC API                          │
│  Create | Write | Read | Delete | Transactions          │
│  ReadBatch | WriteBatch | Indexes | Backup             │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┼────────────┐
        ▼            ▼            ▼
   ┌────────┐  ┌─────────┐  ┌──────────┐
   │ Async  │  │  Hash   │  │   WAL    │
   │ Buffer │  │ Indexes │  │ Protobuf │
   └────┬───┘  └────┬────┘  └────┬─────┘
        │           │            │
        └───────────┼────────────┘
                    ▼
          ┌──────────────────┐
          │  Storage Engine  │
          │  - Encryption    │
          │  - Atomic Writes │
          │  - Deep Copy     │
          └────────┬─────────┘
                   │
        ┌──────────┼──────────┐
        ▼          ▼          ▼
   ┌──────┐  ┌────────┐  ┌────────┐
   │ .db  │  │  .wal  │  │ backup │
   │ File │  │  File  │  │  Dir   │
   └──────┘  └────────┘  └────────┘
```

### Data Isolation

Reads return **defensive deep copies** — stored values are recursively 
copied before being returned. Modifying a returned map, slice, or 
struct never affects the database's internal state. This eliminates 
a whole class of bugs where callers accidentally mutate shared data.

### Conflict Detection

Transactions use optimistic concurrency via an atomic version counter:
- `BeginTransaction` captures current version with deep-copy snapshot
- `CommitTransaction` validates version hasn changed
- If version mismatch → conflict error → caller retries

### Module Structure

- **`polarysdb/`** - Core database implementation
- **`wal/`** - Write-Ahead Log with Protocol Buffers
- **`storage/`** - Storage engine with encryption
- **`tx/`** - Transaction manager with optimistic concurrency
- **`index/`** - Index manager (hash, btree)
- **`mapper/`** - Type mapping (map → struct, primitives)
- **`metrics/`** - Real-time metrics collector
- **`backup/`** - Automatic backup manager
- **`encoding/`** - Serialization with type preservation

## 📖 API Reference

### Core Operations

```go
// Database lifecycle
db, err := polarysdb.Init(key, dirPath, debug)
db, err := polarysdb.InitWithConfig(config)
err := db.Close()
err := db.CloseWithTimeout(timeout)

// Table operations
exists, err := db.Exist(table)
err := db.Create(table)

// Data operations
err := db.Write(table, key, value)
err := db.WriteBatch(table, records)
err := db.Read(table, key, dst)        // dst: *Struct, *map, *primitive
err := db.ReadBatch(table, dst)         // dst: *[]Struct, *[]any, *[]*Struct
err := db.Delete(table, key)

// Index operations
err := db.CreateIndex(table, field)
results, err := db.QueryByIndex(table, field, value)

// Transaction operations
tx, err := db.BeginTransaction()
err := tx.Write(table, key, value)
err := tx.Delete(table, key)
err := db.CommitTransaction(tx)  // conflict detection
err := tx.Rollback()

// Backup operations
err := db.Export(key, path)
err := db.ExportEncrypted(key, path)
err := db.Import(key, path)
err := db.ImportEncrypted(key, path)

// Security
err := db.ChangeKey(oldKey, newKey)

// Monitoring
metrics := db.GetMetrics()
status := db.GetStatus()
```

### Read Destination Types

| Stored Type | Destination | Behavior |
|-------------|-------------|---------|
| `map[string]any` | `*Struct` | Mapper auto-maps fields |
| `map[string]any` | `*map[string]any` | Deep copy (safe to modify) |
| `int`, `string`, `bool` | `*int`, `*string`, `*bool` | Direct assignment |
| `[]byte` | `*[]byte` | Deep copy |
| Any type | Same type `*T` | Deep copy |

### Configuration Options

```go
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
    
    // Timeouts
    OpTimeout    time.Duration  // Single operation timeout
    BatchTimeout time.Duration  // Batch operation timeout
    
    // Monitoring
    Debug          bool
    MetricsEnabled bool
}
```

## 🔧 Building from Source

### Setup

```bash
# Clone repository
git clone https://github.com/polarysfoundation/polarysdb.git
cd polarysdb

# Run setup script
chmod +x setup.sh
./setup.sh

# Or manually
make install-tools
make proto
make build
make test
```

### Development Commands

```bash
# Generate Protocol Buffers
make proto

# Run tests
make test

# Run benchmarks
make bench

# Build binary
make build

# Clean generated files
make clean

# Format code
make fmt

# Run linter
make lint
```

## 🧪 Testing

### Run Tests

```bash
# All tests
make test

# Fast tests only
go test -short ./...

# Specific test
go test -run TestConcurrentWrites

# With coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Benchmarks

```bash
# All benchmarks
make bench

# Specific benchmark
go test -bench=BenchmarkWrite -benchmem ./benchmarks/

# With CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./benchmarks/
go tool pprof cpu.prof
```

## 📈 Performance Tuning

### For High Write Throughput

```go
cfg := polarysdb.DefaultConfig()
cfg.BufferSize = 5000              // Larger buffer
cfg.SaveInterval = 30 * time.Second // Less frequent saves
cfg.WALSyncInterval = 2 * time.Second
db, _ := polarysdb.InitWithConfig(cfg)
```

### For Low Latency Reads

```go
cfg := polarysdb.DefaultConfig()
cfg.EnableIndexes = true
db, _ := polarysdb.InitWithConfig(cfg)

// Create indexes on frequently queried fields
db.CreateIndex("users", "email")
db.CreateIndex("products", "category")
```

### For Production Environments

```go
cfg := polarysdb.DefaultConfig()
cfg.DirPath = "/var/lib/myapp/data"
cfg.BackupDir = "/var/lib/myapp/backups"
cfg.EnableWAL = true
cfg.EnableBackup = true
cfg.BackupInterval = 1 * time.Hour
cfg.SaveInterval = 10 * time.Second
cfg.OpTimeout = 5 * time.Second
cfg.BatchTimeout = 30 * time.Second
cfg.Debug = false
cfg.MetricsEnabled = true
db, _ := polarysdb.InitWithConfig(cfg)
```

## 🛣️ Roadmap

### ✅ Version 2.0.0 (Current)
- [x] Type-safe reads with mapper integration
- [x] Defensive deep copies (no silent data corruption)
- [x] Transaction conflict detection (optimistic concurrency)
- [x] Error returns on closed database
- [x] Atomic key rotation with rollback
- [x] WAL recovery order fix (load → WAL)
- [x] Clean shutdown without panics
- [x] WriteBatch routed through buffer with index updates

### 🚧 Version 2.1.0 (In Progress)
- [ ] Per-table versioning (reduce false conflicts)
- [ ] Configurable isolation levels
- [ ] `ReadRaw()` for zero-copy reads (opt-in, caller must not modify)
- [ ] `CreateIfNotExists()` for idempotent table creation

### 📅 Version 2.2.0 (Planned)
- [ ] LSM-Tree storage engine
- [ ] Incremental snapshots
- [ ] zstd compression
- [ ] B+ Tree indexes
- [ ] Bloom filters
- [ ] Range queries

### 🔮 Version 3.0.0 (Future)
- [ ] MVCC (Multi-Version Concurrency Control)
- [ ] Master-slave replication
- [ ] Horizontal sharding
- [ ] gRPC API
- [ ] Prometheus metrics export
- [ ] Full-text search

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🤝 Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines and our [Security Policy](SECURITY.md) for reporting vulnerabilities.

### Development Workflow

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`make test`)
6. Commit your changes (`git commit -m 'Add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing`)
8. Open a Pull Request

## 🙏 Acknowledgments

- Protocol Buffers team for the excellent serialization system
- Go team for the language and standard library
- Open source community for inspiration

## 📞 Support

- 📧 Email: support@polarys.foundation
- 🐛 Issues: [GitHub Issues](https://github.com/polarysfoundation/polarysdb/issues)
- 💬 Discussions: [GitHub Discussions](https://github.com/polarysfoundation/polarysdb/discussions)
- 📚 Docs: [Documentation](https://docs.polarys.foundation/polarysdb)

## ⭐ Show Your Support

If you find this project useful, please consider giving it a ⭐ on GitHub!

## 📊 Project Stats

- **Language:** Go
- **Lines of Code:** ~8,000
- **Test Coverage:** >85%
- **Performance:** 50,000+ ops/sec
- **Storage Efficiency:** 47% smaller than JSON
- **Active Development:** Yes

## 🔗 Related Projects

- [PolarysDB CLI](https://github.com/polarysfoundation/polarysdb-cli) - Command-line interface
- [PolarysDB GUI](https://github.com/polarysfoundation/polarysdb-gui) - Graphical interface
- [PolarysDB SDK](https://github.com/polarysfoundation/polarysdb-sdk) - Multi-language bindings

---

<p align="center">
  Made with ❤️ by the <a href="https://polarys.foundation">Polarys Foundation</a> team
</p>

<p align="center">
  <a href="#-polarysdb">Back to top</a>
</p>