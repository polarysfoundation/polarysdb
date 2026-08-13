---

# 📜 Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

Changelog

## [2.1.0] — 2026-08-13

### Added

- Encryption and decryption of **WAL** entries if the encryption key is provided.
- Validation of an empty key by checking for a non-zero value with `IsZero()`. 
- Unit Tests for **WAL** Encryption and Decryption. 



### Bug Fixes

- Fixed Insecure exposure of WAL Entries data, exported in plain text in the WAL file.



### Internal Changes

- Adding a key during WAL initialization
- Refactoring and formatting of `encoding/serialize.go`
- Refactoring and formatting of `storage/engine.go`

---



## [2.0.0] — 2026-07-26

### Breaking Changes

- `Read: (any, bool) → (dst any, err error)` Values are now mapped into dst via mapper (supports structs, primitives, maps).Returns defensive deep copies — modifying returned values no longer corrupts internal state.
- `ReadBatch: ([]any, error) → (dst any, err error)` Accepts *[]T, *[]any, *[]*T. Returns mapped deep copies, not shallow references.
- `Exist: bool → (bool, error)` Now returns error when database is closed, allowing callers to distinguish "table doesn't exist" from "database is closed".
- Create: now returns error when table already exists. Previously silently succeeded (idempotent). Callers should check erroror use `Exist()` before `Create()` if idempotent behavior is needed.



### Bug Fixes

- Fixed transaction snapshot isolation (deep copy prevents dirty reads)
- Fixed WriteBatch bypassing write buffer (now routes through buffer with index updates)
- Fixed shutdown race causing lost operations (priority drain in processWriteBuffer)
- Fixed WAL double-close panic (removed CloseBuffer from shutdown sequence)
- Fixed data race on `EncryptionKey` between `ChangeKey` and Export/Import
- Fixed dirtyFlag overwritten to false during concurrent writes (CAS fix)
- Fixed WaitGroup imbalance in startBackgroundWorkers
- Fixed WAL Start called twice during initialization
- Fixed fileOnChange TOCTOU race on dirtyFlag check
- Fixed version counter incrementing on failed operations



### Performance

- `strconv` replaces `fmt.Sprintf` for numeric serialization (3-5x faster)
- Pre-allocated buffers for write serialization
- `engine.getKey()` uses `RLock` instead of `Lock`



### Internal Changes

- Initialization order: load from disk → WAL recovery (prevents data loss)
- deepCopyValue ensures Read/ReadBatch/transactions return independent copies
- `Database.to()` handles same-type deep copy, map→struct via mapper, primitives directly
- Version tracking (atomic counter) enables optimistic concurrency for transactions
- WAL batchWriter and periodicSync now check `ctx.Done()` for clean shutdown
- ChangeKey is now atomic: Save with new key first, rollback on failure



### Dependencies

- Added `reflect` import for `ReadBatch` and `to()`
- Added `encoding/json` import for `deepCopyValue` fallback

---



## [1.1.4] — 2026-04-17



### Added

- Index maintenance on write and delete paths so secondary indexes stay consistent with data changes.
- Helpers for extracting and normalizing indexed field values across operations.



### Changed

- Index manager refactored for clearer handling of multiple value types.
- Index creation logging updated to surface unique-value behavior more accurately.

---



## [1.1.3] — 2026-03-24



### Added

- Added formal Security Policy (`SECURITY.md`)
- Linked Security Policy in `README.md` for vulnerability reporting



### Changed

- Updated dependency `github.com/polarysfoundation/pm-256` to `v1.1.0`

---



## [1.1.2] — 2025-10-30



### Fixed

- Fixed timeout during graceful shutdown (reduced from 30s to <2s)
- Improved worker goroutine cancellation in Close()
- Fixed WAL workers not stopping properly
- Added proper channel closing sequence
- Reduced shutdown timeout from 30s to 8s



### Changed

- Optimized shutdown sequence: HTTP first, then DB
- Added intermediate sleep to allow pending operations to complete
- Improved error messages during shutdown
- WAL now has separate 5s timeout for workers



### Performance

- Shutdown time reduced from 30s to ~1-2s
- No more hanging goroutines
- Proper cleanup of all resources

---



## [1.1.1] — 2025-10-30



### Fixed

- Fixed nil pointer dereference panic during graceful shutdown
- Added nil checks in writeBuffer processing
- Improved channel close handling in processWriteBuffer
- Added timeout buffer before closing database connections



### Changed

- Increased robustness of shutdown sequence
- Better error recovery during database close

---



## [v1.1.0] — 2025-10-27



### 🚀 Added

- Binary Write-Ahead Log (WAL) implementation using **Protocol Buffers** for faster and more reliable persistence.
- **Group commit mechanism** for batching concurrent writes and improving disk I/O performance.
- **Real-time metrics module** exposing operational statistics such as read/write counts and average latency.
- **Automatic backup rotation system**, supporting plaintext and encrypted backups.
- **Hash-based indexing** for O(1) lookup operations on indexed keys.
- **Transactional layer** providing `BeginTransaction`, `Commit`, and `Rollback` with snapshot isolation.
- **Centralized configuration system** (`Config` struct) for tuning WAL, compression, and performance.
- **Comprehensive benchmark suite** with automated performance testing.
- **Makefile commands** for building, testing, linting, and generating protobuf files (`make build`, `make test`, `make proto`).



### 🧰 Changed

- Reworked the **write subsystem** to support asynchronous buffered writes.
- Enhanced **AES-256 encryption** with secure key rotation and file-lock protection.
- Improved **WAL recovery mechanism** to automatically replay incomplete transactions on startup.
- Simplified public API — single-line database initialization and modular imports.
- Refined documentation with advanced usage examples (transactions, backups, metrics).



### 🐞 Fixed

- Fixed concurrency issue in batched writes causing skipped or duplicated records under heavy load.
- Corrected WAL replay logic for interrupted transactions.
- Fixed synchronization race condition in backup rotation.
- Resolved counter overflow issue in metrics during long-running benchmarks.



### ⚙️ Migration

- Fully **backward compatible** with all `v1.0.x` versions.
- WAL files from `v1.0.x` are automatically migrated to the new Protocol Buffers format.
- No manual data migration required.

---



## [v1.0.2] — 2025-10-27



### Added

- Implementation of listening devices for simultaneous changes.
- Correction in the handling of simultaneous goroutines. 
- JSON-based Write-Ahead Log (WAL).
- Import and export of encrypted and unencrypted data.
- Changes and rotation of private keys.

---



## [v1.0.1] — 2025-06-28



### Added

- First implementation for managing simultaneous changes.
- Separation for data encryption and decryption into separate functions.

---



## [v1.0.0] — 2025-03-24



### Added

- Refactoring the use of Create and Exist functions.
- The (common.Key) parameter has been passed directly for better data handling.
- Changed the use of sync.Mutex to sync.RWMutex

---



## [v0.1-beta] — 2025-01-28



### Added

- Initial release of the in-memory database with AES encryption support.
- Added support for creating, reading, updating, and deleting records.
- Implemented batch read functionality.
- Database data is encrypted and saved to a file.
- Added thread-safe access to the database using sync.Mutex.

---


