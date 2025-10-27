---

# 📜 Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [v1.1.0] — 2025-10-27

### 🚀 Added

* Binary Write-Ahead Log (WAL) implementation using **Protocol Buffers** for faster and more reliable persistence.
* **Group commit mechanism** for batching concurrent writes and improving disk I/O performance.
* **Real-time metrics module** exposing operational statistics such as read/write counts and average latency.
* **Automatic backup rotation system**, supporting plaintext and encrypted backups.
* **Hash-based indexing** for O(1) lookup operations on indexed keys.
* **Transactional layer** providing `BeginTransaction`, `Commit`, and `Rollback` with snapshot isolation.
* **Centralized configuration system** (`Config` struct) for tuning WAL, compression, and performance.
* **Comprehensive benchmark suite** with automated performance testing.
* **Makefile commands** for building, testing, linting, and generating protobuf files (`make build`, `make test`, `make proto`).

### 🧰 Changed

* Reworked the **write subsystem** to support asynchronous buffered writes.
* Enhanced **AES-256 encryption** with secure key rotation and file-lock protection.
* Improved **WAL recovery mechanism** to automatically replay incomplete transactions on startup.
* Simplified public API — single-line database initialization and modular imports.
* Refined documentation with advanced usage examples (transactions, backups, metrics).

### 🐞 Fixed

* Fixed concurrency issue in batched writes causing skipped or duplicated records under heavy load.
* Corrected WAL replay logic for interrupted transactions.
* Fixed synchronization race condition in backup rotation.
* Resolved counter overflow issue in metrics during long-running benchmarks.

### ⚙️ Migration

* Fully **backward compatible** with all `v1.0.x` versions.
* WAL files from `v1.0.x` are automatically migrated to the new Protocol Buffers format.
* No manual data migration required.

---

## [v1.0.2] — 2025-09-xx

### Added

* Initial implementation of PolarysDB core engine.
* JSON-based Write-Ahead Log (WAL).
* Basic persistence layer, encryption, and recovery system.
* Documentation and initial performance tests.

---

## [v1.0.0] — 2025-08-xx

### Added

* First public release of PolarysDB.
* Core database structure, logging, and key-value store foundation.

---
