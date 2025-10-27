package tx

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/logger"
)

type Transaction struct {
	ID        string
	Snapshot  map[string]map[string]any
	Changes   map[string]map[string]any
	Committed bool
	Aborted   bool
	CreatedAt time.Time
	mutex     sync.Mutex
}

type Manager struct {
	transactions sync.Map
	counter      uint64
	logger       *logger.Logger
	activeTxs    atomic.Int32
	committedTxs atomic.Uint64
	abortedTxs   atomic.Uint64
}

func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		logger: log,
	}
}

func (m *Manager) Begin(snapshot map[string]map[string]any) *Transaction {
	id := fmt.Sprintf("tx_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&m.counter, 1))

	tx := &Transaction{
		ID:        id,
		Snapshot:  snapshot,
		Changes:   make(map[string]map[string]any),
		CreatedAt: time.Now(),
	}

	m.transactions.Store(id, tx)
	m.activeTxs.Add(1)

	return tx
}

func (m *Manager) Commit(tx *Transaction) (map[string]map[string]any, error) {
	tx.mutex.Lock()
	defer tx.mutex.Unlock()

	if tx.Committed || tx.Aborted {
		return nil, fmt.Errorf("transaction already finalized")
	}

	tx.Committed = true
	m.transactions.Delete(tx.ID)
	m.activeTxs.Add(-1)
	m.committedTxs.Add(1)

	return tx.Changes, nil
}

func (m *Manager) Rollback(tx *Transaction) error {
	tx.mutex.Lock()
	defer tx.mutex.Unlock()

	if tx.Committed || tx.Aborted {
		return fmt.Errorf("transaction already finalized")
	}

	tx.Aborted = true
	m.transactions.Delete(tx.ID)
	m.activeTxs.Add(-1)
	m.abortedTxs.Add(1)

	return nil
}

func (tx *Transaction) Write(table, key string, value any) error {
	tx.mutex.Lock()
	defer tx.mutex.Unlock()

	if tx.Committed || tx.Aborted {
		return fmt.Errorf("transaction already finalized")
	}

	if _, ok := tx.Changes[table]; !ok {
		tx.Changes[table] = make(map[string]any)
	}
	tx.Changes[table][key] = value

	return nil
}

func (tx *Transaction) Delete(table, key string) error {
	tx.mutex.Lock()
	defer tx.mutex.Unlock()

	if tx.Committed || tx.Aborted {
		return fmt.Errorf("transaction already finalized")
	}

	if _, ok := tx.Changes[table]; !ok {
		tx.Changes[table] = make(map[string]any)
	}
	tx.Changes[table][key] = nil

	return nil
}

func (m *Manager) GetStats() map[string]any {
	return map[string]any{
		"active":    m.activeTxs.Load(),
		"committed": m.committedTxs.Load(),
		"aborted":   m.abortedTxs.Load(),
	}
}
