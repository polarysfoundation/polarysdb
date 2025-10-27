package wal

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/logger"
	pb "github.com/polarysfoundation/polarysdb/modules/wal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	OpCreate uint8 = 1
	OpWrite  uint8 = 2
	OpDelete uint8 = 3
	OpCommit uint8 = 4

	maxEntrySize = 10 * 1024 * 1024 // 10MB max per entry
	headerSize   = 8                // 4 bytes length + 4 bytes checksum
)

// Entry representa una entrada en el WAL
type Entry struct {
	OpType    uint8
	Table     string
	Key       string
	Value     any
	TxID      string
	Timestamp int64
}

// Config configuración del WAL
type Config struct {
	Path         string
	SyncInterval time.Duration
	MaxSize      int64
	GroupCommit  bool
	BatchSize    int
}

// WAL representa el Write-Ahead Log
type WAL struct {
	file   *os.File
	writer *bufio.Writer
	path   string
	config *Config
	logger *logger.Logger

	// Buffering and batching
	buffer       chan *Entry
	pendingBatch []*Entry
	batchMutex   sync.Mutex

	// Metrics
	size    int64
	entries uint64
	flushes uint64

	// Lifecycle
	closed     bool
	closeMutex sync.Mutex
	wg         sync.WaitGroup
}

// New crea una nueva instancia de WAL
func New(cfg *Config, log *logger.Logger) (*WAL, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Abrir archivo en modo append
	f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	batchSize := cfg.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}

	w := &WAL{
		file:         f,
		writer:       bufio.NewWriterSize(f, 256*1024), // 256KB buffer
		path:         cfg.Path,
		config:       cfg,
		logger:       log,
		buffer:       make(chan *Entry, 1000),
		pendingBatch: make([]*Entry, 0, batchSize),
		size:         info.Size(),
	}

	return w, nil
}

// Start inicia los workers del WAL
func (w *WAL) Start() {
	w.wg.Add(2)
	go w.batchWriter()
	go w.periodicSync()
}

// Append agrega una entrada al buffer del WAL
func (w *WAL) Append(entry *Entry) error {
	w.closeMutex.Lock()
	if w.closed {
		w.closeMutex.Unlock()
		return fmt.Errorf("WAL is closed")
	}
	w.closeMutex.Unlock()

	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixNano()
	}

	select {
	case w.buffer <- entry:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("WAL buffer full")
	}
}

// batchWriter procesa entradas en lotes
func (w *WAL) batchWriter() {
	defer w.wg.Done()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-w.buffer:
			if !ok {
				w.flushBatch()
				return
			}

			w.batchMutex.Lock()
			w.pendingBatch = append(w.pendingBatch, entry)
			shouldFlush := len(w.pendingBatch) >= w.config.BatchSize
			w.batchMutex.Unlock()

			if shouldFlush {
				w.flushBatch()
			}

		case <-ticker.C:
			w.flushBatch()
		}
	}
}

// flushBatch escribe el lote pendiente al disco
func (w *WAL) flushBatch() error {
	w.batchMutex.Lock()
	if len(w.pendingBatch) == 0 {
		w.batchMutex.Unlock()
		return nil
	}

	batch := w.pendingBatch
	w.pendingBatch = make([]*Entry, 0, w.config.BatchSize)
	w.batchMutex.Unlock()

	// Group Commit: escribir todas las entradas juntas
	for _, entry := range batch {
		if err := w.writeEntry(entry); err != nil {
			w.logger.Errorf("Failed to write WAL entry: %v", err)
			return err
		}
	}

	// Flush del buffer
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL: %w", err)
	}

	w.flushes++

	// Sync si está configurado para group commit
	if w.config.GroupCommit {
		if err := w.file.Sync(); err != nil {
			return fmt.Errorf("failed to sync WAL: %w", err)
		}
	}

	return nil
}

// writeEntry escribe una entrada individual al WAL usando Protocol Buffers
func (w *WAL) writeEntry(entry *Entry) error {
	// Convertir a protobuf
	pbEntry := &pb.WALEntry{
		OpType:    uint32(entry.OpType),
		Table:     entry.Table,
		Key:       entry.Key,
		TxId:      entry.TxID,
		Timestamp: entry.Timestamp,
	}

	// Serializar valor si existe
	if entry.Value != nil {
		valueBytes, err := serializeValue(entry.Value)
		if err != nil {
			return fmt.Errorf("failed to serialize value: %w", err)
		}
		pbEntry.Value = valueBytes
	}

	// Marshal protobuf - AQUÍ SE USA EL PROTOBUF REAL
	data, err := proto.Marshal(pbEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	if len(data) > maxEntrySize {
		return fmt.Errorf("entry too large: %d bytes", len(data))
	}

	// Calcular checksum CRC32
	checksum := crc32.ChecksumIEEE(data)

	// Escribir header: [length(4)][checksum(4)]
	header := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(header[4:8], checksum)

	// Escribir al buffer
	if _, err := w.writer.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}
	if _, err := w.writer.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	w.size += int64(headerSize + len(data))
	w.entries++

	// Verificar si necesitamos rotar
	if w.size > w.config.MaxSize {
		go w.rotate()
	}

	return nil
}

// periodicSync hace sync periódico del archivo
func (w *WAL) periodicSync() {
	defer w.wg.Done()

	if w.config.GroupCommit {
		return // Group commit ya hace sync
	}

	ticker := time.NewTicker(w.config.SyncInterval)
	defer ticker.Stop()

	for range ticker.C {
		w.closeMutex.Lock()
		if w.closed {
			w.closeMutex.Unlock()
			return
		}
		w.closeMutex.Unlock()

		if err := w.file.Sync(); err != nil {
			w.logger.Errorf("WAL sync failed: %v", err)
		}
	}
}

// ReadAll lee todas las entradas del WAL
func (w *WAL) ReadAll() ([]*Entry, error) {
	f, err := os.Open(w.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	entries := make([]*Entry, 0)

	for {
		entry, err := readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			w.logger.Warnf("Corrupted WAL entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// readEntry lee una entrada individual del WAL
func readEntry(reader *bufio.Reader) (*Entry, error) {
	// Leer header
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}

	length := binary.LittleEndian.Uint32(header[0:4])
	expectedChecksum := binary.LittleEndian.Uint32(header[4:8])

	if length > maxEntrySize {
		return nil, fmt.Errorf("entry too large: %d bytes", length)
	}

	// Leer datos
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, err
	}

	// Verificar checksum
	actualChecksum := crc32.ChecksumIEEE(data)
	if actualChecksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %d, got %d", expectedChecksum, actualChecksum)
	}

	// Unmarshal protobuf - AQUÍ SE USA EL PROTOBUF REAL
	pbEntry := &pb.WALEntry{}
	if err := proto.Unmarshal(data, pbEntry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	// Convertir a Entry
	entry := &Entry{
		OpType:    uint8(pbEntry.OpType),
		Table:     pbEntry.Table,
		Key:       pbEntry.Key,
		TxID:      pbEntry.TxId,
		Timestamp: pbEntry.Timestamp,
	}

	// Deserializar valor si existe
	if len(pbEntry.Value) > 0 {
		value, err := deserializeValue(pbEntry.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize value: %w", err)
		}
		entry.Value = value
	}

	return entry, nil
}

// rotate rota el archivo WAL cuando es muy grande
func (w *WAL) rotate() error {
	w.closeMutex.Lock()
	defer w.closeMutex.Unlock()

	if w.closed {
		return nil
	}

	w.logger.Info("Rotating WAL file...")

	// Flush pendiente
	w.flushBatch()
	w.writer.Flush()
	w.file.Sync()

	// Cerrar archivo actual
	if err := w.file.Close(); err != nil {
		return err
	}

	// Renombrar a backup con timestamp
	backupPath := fmt.Sprintf("%s.%d.backup", w.path, time.Now().Unix())
	if err := os.Rename(w.path, backupPath); err != nil {
		return err
	}

	// Abrir nuevo archivo
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	w.file = f
	w.writer = bufio.NewWriterSize(f, 256*1024)
	w.size = 0

	w.logger.Infof("WAL rotated successfully. Old file: %s", backupPath)

	// Eliminar backup antiguo después de un tiempo
	go func() {
		time.Sleep(30 * time.Second)
		if err := os.Remove(backupPath); err != nil {
			w.logger.Warnf("Failed to remove old WAL backup: %v", err)
		}
	}()

	return nil
}

// Truncate elimina entradas antiguas del WAL (después de snapshot)
func (w *WAL) Truncate() error {
	w.closeMutex.Lock()
	defer w.closeMutex.Unlock()

	if w.closed {
		return fmt.Errorf("WAL is closed")
	}

	w.logger.Info("Truncating WAL...")

	// Flush y cerrar
	w.flushBatch()
	w.writer.Flush()
	w.file.Sync()
	w.file.Close()

	// Eliminar archivo
	if err := os.Remove(w.path); err != nil {
		return err
	}

	// Crear nuevo archivo vacío
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	w.file = f
	w.writer = bufio.NewWriterSize(f, 256*1024)
	w.size = 0
	w.entries = 0

	w.logger.Info("WAL truncated successfully")
	return nil
}

// GetStats retorna estadísticas del WAL
func (w *WAL) GetStats() map[string]any {
	return map[string]any{
		"size_bytes": w.size,
		"entries":    w.entries,
		"flushes":    w.flushes,
		"path":       w.path,
	}
}

// Close cierra el WAL de forma segura
func (w *WAL) Close() error {
	w.closeMutex.Lock()
	if w.closed {
		w.closeMutex.Unlock()
		return nil
	}
	w.closed = true
	w.closeMutex.Unlock()

	// Cerrar canal
	close(w.buffer)

	// Esperar workers
	w.wg.Wait()

	// Flush final
	if w.writer != nil {
		w.writer.Flush()
	}
	if w.file != nil {
		w.file.Sync()
		w.file.Close()
	}

	w.logger.Info("WAL closed successfully")
	return nil
}

// ============================================================================
// Helper functions para serialización de valores
// ============================================================================

// serializeValue convierte un valor Go a bytes
func serializeValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return []byte(fmt.Sprintf("%v", v)), nil
	case float32, float64:
		return []byte(fmt.Sprintf("%v", v)), nil
	case bool:
		if v {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	default:
		// Para tipos complejos (maps, structs), usar JSON
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal complex value: %w", err)
		}
		return data, nil
	}
}

// deserializeValue convierte bytes de vuelta a un valor Go
func deserializeValue(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Intentar deserializar como JSON primero (para tipos complejos)
	var jsonValue any
	if err := json.Unmarshal(data, &jsonValue); err == nil {
		return jsonValue, nil
	}

	// Si falla, retornar como string
	return string(data), nil
}
