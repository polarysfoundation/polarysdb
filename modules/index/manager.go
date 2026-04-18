package index

import (
	"fmt"
	"reflect"
	"strings"
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
		return nil // Already exists
	}

	idx := &Index{
		data: make(map[string][]string),
	}

	// Build index from existing data
	for key, value := range tableData {
		if fieldValue, ok := extractFieldValue(value, field); ok {
			fieldStr := formatIndexValue(fieldValue)
			idx.data[fieldStr] = append(idx.data[fieldStr], key)
		}
	}

	m.indexes[table][field] = idx
	m.logger.Infof("Index created: %s.%s (%d unique values)", table, field, len(idx.data))
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

	valueStr := formatIndexValue(value)

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
		return nil // No indexes for this table
	}

	idx, ok := tableIndexes[field]
	if !ok {
		m.mutex.RUnlock()
		return nil // No index for this field
	}
	m.mutex.RUnlock()

	idx.mutex.Lock()
	defer idx.mutex.Unlock()

	// Remove from old value
	if oldValue != nil {
		if oldFieldValue, ok := extractFieldValue(oldValue, field); ok {
			oldStr := formatIndexValue(oldFieldValue)
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
	}

	// Add to new value
	if newValue != nil {
		if newFieldValue, ok := extractFieldValue(newValue, field); ok {
			newStr := formatIndexValue(newFieldValue)
			idx.data[newStr] = append(idx.data[newStr], key)
		}
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

	if fieldValue, ok := extractFieldValue(value, field); ok {
		valueStr := formatIndexValue(fieldValue)
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

// GetIndexedFields returns all fields currently indexed for a given table.
func (m *Manager) GetIndexedFields(table string) []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	tableIndexes, ok := m.indexes[table]
	if !ok {
		return nil
	}

	fields := make([]string, 0, len(tableIndexes))
	for field := range tableIndexes {
		fields = append(fields, field)
	}
	return fields
}

// Helper functions for index extraction and formatting

func extractFieldValue(obj any, field string) (any, bool) {
	if obj == nil {
		return nil, false
	}

	// 1. Try as map[string]any
	if m, ok := obj.(map[string]any); ok {
		val, exists := m[field]
		return val, exists
	}

	// 2. Try as struct via reflection
	v := reflect.ValueOf(obj)
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, false
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		structField := t.Field(i)

		// Check JSON tag first (most common for DB models)
		jsonTag := structField.Tag.Get("json")
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] == field {
				return v.Field(i).Interface(), true
			}
		}

		// Fallback to name match
		if strings.EqualFold(structField.Name, field) {
			return v.Field(i).Interface(), true
		}
	}

	return nil, false
}

func formatIndexValue(val any) string {
	if val == nil {
		return "null"
	}

	// Handle specific common types to ensure consistent string representation
	// especially for custom types that might not implement String()
	v := reflect.ValueOf(val)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "null"
		}
		v = v.Elem()
	}

	// If it implements Stringer, use it (common for Addresses, Hashes)
	if s, ok := val.(interface{ String() string }); ok {
		return s.String()
	}

	// If it's a fixed-size byte array (like common.Address or common.Hash aliases),
	// but doesn't implement Stringer on the alias, we should try to get it if it's 20 or 32 bytes.
	if v.Kind() == reflect.Array && (v.Len() == 20 || v.Len() == 32) {
		bytes := make([]byte, v.Len())
		reflect.Copy(reflect.ValueOf(bytes), v)
		
		// If it's 20 bytes, it's likely an address
		if v.Len() == 20 {
			return strings.ToLower(fmt.Sprintf("0x%x", bytes))
		}
		// If it's 32 bytes, it's likely a hash
		if v.Len() == 32 {
			return strings.ToLower(fmt.Sprintf("0x%x", bytes))
		}
	}

	return strings.ToLower(fmt.Sprintf("%v", val))
}