package polarysdb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// syncableWAL allows the WAL to be explicitly synced if the concrete type
// of db.wal supports it. If it does not support it, Flush skips syncing the
// WAL without failing (the data is already safe in the snapshot).
type syncableWAL interface {
	Sync() error
}

// Flush safely persists the ENTIRE state of the database to disk.
//
// Guarantees:
//   1. Waits for all pending operations in writeBuffer to be applied to
//      db.data (pendingOps == 0).
//   2. Takes a W lock for the entire save+verify cycle — blocks new
//      writes and reads to prevent the verification from being invalid.
//   3. Persists the data to the storage engine (encrypted).
//   4. Verifies integrity: reloads from disk and compares (via JSON to
//      normalize time.Time without the monotonic clock).
//   5. Explicitly syncs the WAL if the concrete type supports it.
//   6. Clears dirtyFlag ONLY if all previous steps succeeded.
//
// It is blocking and expensive (duplicates I/O due to verification). During
// execution, all reads and writes are blocked.
//
// Use cases:
//   - Before os.Exit() or graceful signal handlers.
//   - After a large import WriteBatch.
//   - Before external snapshots / backups.
//   - In tests that require verifiable persisted state.
//
// For periodic non-blocking flush, use the background worker (periodicSave).
func (db *Database) Flush() error {
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	start := time.Now()

	// 1. Wait for all pending ops to be applied to db.data.
	//    This is done BEFORE taking the W lock to avoid deadlock:
	//    processWriteBuffer needs the W lock to apply ops, so if we held
	//    the lock, the ops would never drain.
	if err := db.waitForPendingOps(); err != nil {
		return fmt.Errorf("flush: failed waiting for pending ops: %w", err)
	}

	// 2. Take the exclusive W lock. Blocks:
	//    - processWriteBuffer (new applications of writeBuffer)
	//    - Read / ReadBatch / Exist (new reads)
	//    - periodicSave (which uses flushToDisk with RLock)
	//    This guarantees that db.data does not mutate during save+verify.
	db.dataMutex.Lock()
	defer db.dataMutex.Unlock()

	// Re-check closed under the lock: the state may have changed between the
	// initial check and acquiring the lock (race with Close).
	if db.closed.Load() {
		return fmt.Errorf("database is closed")
	}

	// 3. Early-exit if there are no unpersisted changes.
	if !db.dirtyFlag.Load() {
		return nil
	}

	// 4. Persist to disk. storage.Save encrypts and serializes.
	if err := db.storage.Save(db.data); err != nil {
		// Do not clear dirtyFlag — the next periodicSave will retry.
		return fmt.Errorf("flush: save failed: %w", err)
	}

	// 5. Verify integrity: reload from disk and compare with db.data.
	//    Requires the caller to hold the W lock (which we already do).
	if err := db.verifyFlush(); err != nil {
		return fmt.Errorf("flush: verification failed: %w", err)
	}

	// 6. Explicitly sync the WAL if the concrete type supports it.
	//    The data is already safe in the encrypted snapshot; syncing the
	//    WAL ensures that the transaction log is also durable on disk
	//    (not just in the OS buffer).
	if db.wal != nil {
		if s, ok := interface{}(db.wal).(syncableWAL); ok {
			if err := s.Sync(); err != nil {
				// Do not fail the entire flush: the data is already on disk.
				// Log for operational visibility.
				db.logger.Errorf(
					"flush: WAL sync failed (data is still safe on disk snapshot): %v",
					err,
				)
				if db.metrics != nil {
					db.metrics.IncrementFailedOps()
				}
			}
		}
		// If the WAL does not implement Sync(), we do nothing: the WAL's
		// background worker (started in startBackgroundWorkers) syncs it
		// periodically according to SyncInterval.
	}

	// 7. Mark as clean ONLY if everything above succeeded.
	db.dirtyFlag.Store(false)
	db.lastSave.Store(time.Now())

	if db.metrics != nil {
		db.metrics.RecordSaveDuration(time.Since(start))
	}

	db.logger.Infof("Flush completed successfully (took %s)", time.Since(start))
	return nil
}

// waitForPendingOps waits until all operations queued in writeBuffer have
// been applied to db.data (pendingOps == 0).
//
// Uses short polling (5ms) instead of condition variables to keep things
// simple and avoid introducing a dependency on sync.Cond.
//
// Timeout configurable via db.config.BatchTimeout (default 30s).
// If the context is canceled (database shutting down), returns an error.
func (db *Database) waitForPendingOps() error {
	if db.pendingOps.Load() == 0 {
		return nil
	}

	timeout := time.NewTimer(db.config.BatchTimeout)
	defer timeout.Stop()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		if db.pendingOps.Load() == 0 {
			return nil
		}
		select {
		case <-db.ctx.Done():
			return fmt.Errorf(
				"database is shutting down (pending=%d ops not applied)",
				db.pendingOps.Load(),
			)
		case <-timeout.C:
			return fmt.Errorf(
				"timeout waiting for %d pending ops to drain (BatchTimeout=%s)",
				db.pendingOps.Load(), db.config.BatchTimeout,
			)
		case <-ticker.C:
			// continue polling
		}
	}
}

// verifyFlush reloads the data from disk and compares it with db.data
// to guarantee that the flush was successful and the persisted data is
// consistent with the in-memory state.
//
// REQUIRES: the caller already holds db.dataMutex.Lock() (W lock).
//
// Uses comparison via JSON marshal instead of reflect.DeepEqual because:
//   - reflect.DeepEqual on time.Time considers the monotonic clock, which
//     is lost upon serialization → false negatives.
//   - JSON marshal normalizes time.Time to RFC3339 and sorts map keys,
//     giving a deterministic and semantically correct comparison.
//
// Cost: O(n) over the entire dataset (marshal + reload + marshal + compare).
func (db *Database) verifyFlush() error {
	data, modTime, err := db.storage.Load()
	if err != nil {
		return fmt.Errorf("reload after save failed: %w", err)
	}

	// Comparison via JSON to normalize types (especially time.Time).
	aBytes, err := json.Marshal(db.data)
	if err != nil {
		return fmt.Errorf("marshal in-memory data failed: %w", err)
	}
	bBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal reloaded data failed: %w", err)
	}

	if !bytes.Equal(aBytes, bBytes) {
		// This would indicate corruption in storage.Save or storage.Load.
		// Do not clear dirtyFlag — the next flush will retry.
		return fmt.Errorf(
			"flushed data does not match in-memory data "+
				"(possible corruption during save/load cycle)",
		)
	}

	// Update lastLoaded: we just read from disk and the modTime
	// corresponds to the file we wrote. This prevents fileOnChange
	// from triggering an unnecessary reload on its next tick.
	db.lastLoaded.Store(modTime)

	return nil
}