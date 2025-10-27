package index

import (
	"fmt"
	"sync"

	"github.com/polarysfoundation/polarysdb/modules/logger"
)

type Index struct {
	data  map[string][]string // field_value -> [keys]
	mutex sync.RWMutex
}

type Manager struct {
	indexes map[string]map[string]*Index // table -> field -> index
	mutex   sync.RWMutex
	logger  *logger.Logger
}

func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		indexes: make(map[string]map[string]*Index),
		logger:  log,
	}
}

func (m *Manager) CreateIndex(table, field string, tableData map[string]any) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.indexes[table]; !ok {
		m.indexes[table] = make(map[string]*Index)
	}

	if _, ok := m.indexes[table][field]; ok {
		return nil // Ya existe
	}

	idx := &Index{
		data: make(map[string][]string),
	}

	// Construir índice desde datos existentes
	for key, value := range tableData {
		if m, ok := value.(map[string]any); ok {
			if fieldValue, ok := m[field]; ok {
				fieldStr := fmt.Sprintf("%v", fieldValue)
				idx.data[fieldStr] = append(idx.data[fieldStr], key)
			}
		}
	}

	m.indexes[table][field] = idx
	m.logger.Infof("Index created: %s.%s (%d entries)", table, field, len(idx.data))
	return nil
}

func (m *Manager) Query(table, field string, value any) ([]string, error) {
	m.mutex.RLock()
	tableIndexes, ok := m.indexes[table]
	if !ok {
		m.mutex.RUnlock()
		return nil, fmt.Errorf("no indexes for table %s", table)
	}

	idx, ok := tableIndexes[field]
	if !ok {
		m.mutex.RUnlock()
		return nil, fmt.Errorf("no index for field %s.%s", table, field)
	}
	m.mutex.RUnlock()

	valueStr := fmt.Sprintf("%v", value)

	idx.mutex.RLock()
	keys, ok := idx.data[valueStr]
	idx.mutex.RUnlock()

	if !ok {
		return []string{}, nil
	}

	result := make([]string, len(keys))
	copy(result, keys)
	return result, nil
}

func (m *Manager) UpdateIndex(table, field, key string, oldValue, newValue any) error {
	m.mutex.RLock()
	tableIndexes, ok := m.indexes[table]
	if !ok {
		m.mutex.RUnlock()
		return nil // No hay índices para esta tabla
	}

	idx, ok := tableIndexes[field]
	if !ok {
		m.mutex.RUnlock()
		return nil // No hay índice para este campo
	}
	m.mutex.RUnlock()

	idx.mutex.Lock()
	defer idx.mutex.Unlock()

	// Remover de valor anterior
	if oldValue != nil {
		oldStr := fmt.Sprintf("%v", oldValue)
		if keys, ok := idx.data[oldStr]; ok {
			for i, k := range keys {
				if k == key {
					idx.data[oldStr] = append(keys[:i], keys[i+1:]...)
					break
				}
			}
			if len(idx.data[oldStr]) == 0 {
				delete(idx.data, oldStr)
			}
		}
	}

	// Agregar a nuevo valor
	if newValue != nil {
		newStr := fmt.Sprintf("%v", newValue)
		idx.data[newStr] = append(idx.data[newStr], key)
	}

	return nil
}

func (m *Manager) DeleteFromIndex(table, field, key string, value any) error {
	m.mutex.RLock()
	tableIndexes, ok := m.indexes[table]
	if !ok {
		m.mutex.RUnlock()
		return nil
	}

	idx, ok := tableIndexes[field]
	if !ok {
		m.mutex.RUnlock()
		return nil
	}
	m.mutex.RUnlock()

	idx.mutex.Lock()
	defer idx.mutex.Unlock()

	valueStr := fmt.Sprintf("%v", value)
	if keys, ok := idx.data[valueStr]; ok {
		for i, k := range keys {
			if k == key {
				idx.data[valueStr] = append(keys[:i], keys[i+1:]...)
				break
			}
		}
		if len(idx.data[valueStr]) == 0 {
			delete(idx.data, valueStr)
		}
	}

	return nil
}

func (m *Manager) DropIndex(table, field string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if tableIndexes, ok := m.indexes[table]; ok {
		delete(tableIndexes, field)
		m.logger.Infof("Index dropped: %s.%s", table, field)
	}

	return nil
}

func (m *Manager) GetStats() map[string]any {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalIndexes := 0
	totalEntries := 0

	for _, tableIndexes := range m.indexes {
		for _, idx := range tableIndexes {
			totalIndexes++
			idx.mutex.RLock()
			totalEntries += len(idx.data)
			idx.mutex.RUnlock()
		}
	}

	return map[string]any{
		"total_indexes": totalIndexes,
		"total_entries": totalEntries,
	}
}