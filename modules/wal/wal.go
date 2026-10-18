package wal

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/v2/modules/common"
	"github.com/polarysfoundation/polarysdb/v2/modules/crypto"
	"github.com/polarysfoundation/polarysdb/v2/modules/encoding"
	"github.com/polarysfoundation/polarysdb/v2/modules/logger"
	pb "github.com/polarysfoundation/polarysdb/v2/modules/wal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	OpCreate uint8 = 1
	OpWrite  uint8 = 2
	OpDelete uint8 = 3
	OpCommit uint8 = 4
	OpBatch  uint8 = 5

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
	Key          common.Key
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

	// PATCH 2: flag atómico para coordinar rotaciones.
	// writeEntry lo levanta cuando size > MaxSize; rotateWatcher lo baja
	// cuando termina la rotación. Evita múltiples rotaciones solapadas.
	rotating atomic.Bool

	bufferClosed bool

	ctx    context.Context
	cancel context.CancelFunc
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
	if w.ctx == nil {
		w.logger.Warn("WAL context not set, using background context")
		w.ctx = context.Background()
	}
	// PATCH 2: agregar rotateWatcher como worker adicional.
	w.wg.Add(3)
	go w.batchWriter()
	go w.periodicSync()
	go w.rotateWatcher()
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
		// Prioridad: drain buffer primero
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

		default:
			// Buffer vacío — verificar contexto
			select {
			case <-w.ctx.Done():
				w.flushBatch()
				return
			case entry, ok := <-w.buffer:
				if !ok {
					w.flushBatch()
					return
				}

				w.batchMutex.Lock()
				w.pendingBatch = append(w.pendingBatch, entry)
				w.batchMutex.Unlock()

			case <-ticker.C:
				w.flushBatch()
			}
		}
	}
}

// flushBatch escribe el lote pendiente al disco.
// Wrapper público que toma el lock; la lógica real está en flushBatchLocked.
func (w *WAL) flushBatch() error {
	w.batchMutex.Lock()
	defer w.batchMutex.Unlock()
	return w.flushBatchLocked()
}

// flushBatchLocked es idéntico a flushBatch pero asume que el caller ya
// tiene batchMutex tomado. Evita reentrancia desde rotate().
func (w *WAL) flushBatchLocked() error {
	if len(w.pendingBatch) == 0 {
		return nil
	}

	batch := w.pendingBatch
	w.pendingBatch = make([]*Entry, 0, w.config.BatchSize)

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

// writeEntry escribe una entrada individual al WAL usando Protocol Buffers.
// REQUIERE: el caller debe tener batchMutex tomado (vía flushBatch o
// flushBatchLocked). No toma ningún lock internamente.
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
		valueBytes, err := encoding.SerializeValue(entry.Value)
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

	if !w.config.Key.IsZero() {
		data, err = crypto.Encrypt(data, w.config.Key)
		if err != nil {
			return fmt.Errorf("failed to encrypt entry: %v", err)
		}
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

	// PATCH 2: si excedemos MaxSize, levantar el flag atómico para que
	// rotateWatcher procese la rotación de forma síncrona y segura.
	// CompareAndSwap garantiza que solo una goroutine levanta el flag,
	// evitando múltiples rotaciones solapadas.
	if w.size > w.config.MaxSize {
		w.rotating.CompareAndSwap(false, true)
	}

	return nil
}

// periodicSync hace sync periódico del archivo
func (w *WAL) periodicSync() {
	defer w.wg.Done()

	if w.config.GroupCommit {
		return
	}

	ticker := time.NewTicker(w.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.closeMutex.Lock()
			if w.closed {
				w.closeMutex.Unlock()
				return
			}
			w.closeMutex.Unlock()

			// PATCH 1: flush del bufio.Writer al kernel page cache
			// ANTES de file.Sync(). Sin esto, hasta 256KB de entries
			// "aceptadas" por Append() no llegan al kernel y file.Sync()
			// no las toca — se pierden ante crash del proceso.
			//
			// Tomamos batchMutex brevemente para que no se entrelace
			// con writes parciales de flushBatch/writeEntry (que pueden
			// estar a mitad de escribir un entry: header escrito pero
			// data no, lo que dejaría el bufio.Writer en estado
			// inconsistente).
			w.batchMutex.Lock()
			flushErr := w.writer.Flush()
			w.batchMutex.Unlock()

			if flushErr != nil {
				w.logger.Errorf("WAL writer flush failed: %v", flushErr)
				continue
			}

			if err := w.file.Sync(); err != nil {
				w.logger.Errorf("WAL sync failed: %v", err)
			}

		case <-w.ctx.Done():
			// Flush final antes de salir
			w.batchMutex.Lock()
			w.writer.Flush()
			w.batchMutex.Unlock()
			w.file.Sync()
			return
		}
	}
}

// PATCH 2: rotateWatcher es un worker dedicado que procesa rotaciones
// de forma síncrona y segura. Reemplaza el `go w.rotate()` original que
// tenía race condition con flushBatch/writeEntry.
//
// Cada 100ms chequea el flag `rotating`. Si está levantado, llama
// rotate() (que toma los locks necesarios en orden consistente) y lo
// baja al terminar. Si rotate() falla, lo loguea y baja el flag de
// todas formas para no atascar rotaciones futuras.
func (w *WAL) rotateWatcher() {
	defer w.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if !w.rotating.Load() {
				continue
			}
			if err := w.rotate(); err != nil {
				w.logger.Errorf("WAL rotation failed: %v", err)
			}
			w.rotating.Store(false)
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
		entry, err := w.readEntry(reader)
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
func (w *WAL) readEntry(reader *bufio.Reader) (*Entry, error) {
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

	if !w.config.Key.IsZero() {
		var err error
		data, err = crypto.Decrypt(data, w.config.Key)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt entry: %v", err)
		}
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
		value, err := encoding.DeserializeValue(pbEntry.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize value: %w", err)
		}
		entry.Value = value
	}

	return entry, nil
}

// rotate rota el archivo WAL cuando es muy grande.
//
// PATCH 2: ahora toma closeMutex → batchMutex en orden consistente y
// usa flushBatchLocked() para evitar reentrancia. Es llamada de forma
// síncrona por rotateWatcher() (no más `go w.rotate()` que tenía race
// condition con flushBatch/writeItem).
//
// Lock ordering en todo el archivo: closeMutex → batchMutex. Nadie toma
// batchMutex → closeMutex, así que no hay deadlock potential.
func (w *WAL) rotate() error {
	w.closeMutex.Lock()
	defer w.closeMutex.Unlock()

	if w.closed {
		return nil
	}

	w.logger.Info("Rotating WAL file...")

	// Tomar batchMutex DESPUÉS de closeMutex (orden consistente).
	// flushBatch solo toma batchMutex (no closeMutex), así que no hay
	// deadlock.
	w.batchMutex.Lock()
	defer w.batchMutex.Unlock()

	// Flush pendiente bajo el lock. Usar flushBatchLocked para no
	// reentrar batchMutex.
	if err := w.flushBatchLocked(); err != nil {
		return fmt.Errorf("rotate: flushBatch failed: %w", err)
	}
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("rotate: writer flush failed: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("rotate: file sync failed: %w", err)
	}

	// Cerrar archivo actual
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("rotate: close failed: %w", err)
	}

	// Renombrar a backup con timestamp
	backupPath := fmt.Sprintf("%s.%d.backup", w.path, time.Now().Unix())
	if err := os.Rename(w.path, backupPath); err != nil {
		return fmt.Errorf("rotate: rename failed: %w", err)
	}

	// Abrir nuevo archivo
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("rotate: open new file failed: %w", err)
	}

	w.file = f
	w.writer = bufio.NewWriterSize(f, 256*1024)
	w.size = 0

	w.logger.Infof("WAL rotated successfully. Old file: %s", backupPath)

	// Eliminar backup antiguo después de un tiempo
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			if err := os.Remove(backupPath); err != nil {
				w.logger.Warnf("Failed to remove old WAL backup: %v", err)
			}
		case <-w.ctx.Done():
			w.logger.Info("Rotation cleanup cancelled, not removing backup")
			return
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

	// PATCH 3: esperar a que batchWriter drene w.buffer antes de
	// flushBatch. Sin esto, entries en el channel se pierden cuando
	// os.Remove() borra el archivo antes de que se escriban.
	//
	// waitForBufferDrain no toma batchMutex (solo hace polling sobre
	// len(w.buffer)), así que no hay deadlock con el closeMutex que
	// ya tenemos.
	if err := w.waitForBufferDrain(5 * time.Second); err != nil {
		// No abortar el truncate, pero loguear: el caller decide
		// si la pérdida potencial de entries es aceptable.
		w.logger.Warnf("WAL Truncate: buffer drain timeout, some entries may be lost: %v", err)
	}

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

// CloseBuffer cierra solo el canal de buffer para que Append falle rápido
func (w *WAL) CloseBuffer() {
	w.closeMutex.Lock()
	if w.bufferClosed || w.buffer == nil {
		w.closeMutex.Unlock()
		return
	}
	close(w.buffer)
	w.bufferClosed = true
	w.closeMutex.Unlock()
}

// Close cierra el WAL de forma segura
func (w *WAL) Close() error {
	w.closeMutex.Lock()
	if w.closed {
		w.closeMutex.Unlock()
		return nil
	}
	w.closed = true

	// Solo cerrar el buffer si no se cerró antes
	if !w.bufferClosed {
		close(w.buffer)
		w.bufferClosed = true
	}
	w.closeMutex.Unlock()

	// Cancelar contexto para detener workers de limpieza (rotate)
	if w.cancel != nil {
		w.cancel()
	}

	w.logger.Info("Closing WAL...")

	// Esperar a que batchWriter, periodicSync y rotateWatcher terminen
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		w.logger.Warn("WAL workers did not finish in time")
	}

	// Flush final
	if w.writer != nil {
		w.flushBatch()
		w.writer.Flush()
	}

	if w.file != nil {
		w.file.Sync()
		w.file.Close()
	}

	w.logger.Info("WAL closed")
	return nil
}

func (w *WAL) SetContext(ctx context.Context) {
	w.ctx, w.cancel = context.WithCancel(ctx)
}