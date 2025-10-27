package metrics

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/logger"
)

type Collector struct {
	totalReads      atomic.Uint64
	totalWrites     atomic.Uint64
	totalDeletes    atomic.Uint64
	failedOps       atomic.Uint64
	avgReadLatency  atomic.Int64
	avgWriteLatency atomic.Int64
	dataSize        atomic.Int64
	walSize         atomic.Int64
	uptime          time.Time
	lastSave        time.Time
	saveDuration    time.Duration
	mutex           sync.Mutex
}

type Snapshot struct {
	TotalReads      uint64
	TotalWrites     uint64
	TotalDeletes    uint64
	FailedOps       uint64
	AvgReadLatency  time.Duration
	AvgWriteLatency time.Duration
	DataSize        int64
	WALSize         int64
	Uptime          time.Time
	LastSave        time.Time
	SaveDuration    time.Duration
}

func NewCollector() *Collector {
	return &Collector{
		uptime: time.Now(),
	}
}

func (c *Collector) Start(ctx context.Context, log *logger.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Infof("Metrics - Reads: %d, Writes: %d, Deletes: %d, Failed: %d",
				c.totalReads.Load(),
				c.totalWrites.Load(),
				c.totalDeletes.Load(),
				c.failedOps.Load())
		}
	}
}

func (c *Collector) IncrementReads() {
	c.totalReads.Add(1)
}

func (c *Collector) IncrementWrites(count uint64) {
	c.totalWrites.Add(count)
}

func (c *Collector) IncrementDeletes() {
	c.totalDeletes.Add(1)
}

func (c *Collector) IncrementFailedOps() {
	c.failedOps.Add(1)
}

func (c *Collector) RecordReadLatency(duration time.Duration) {
	// Promedio móvil simple
	current := c.avgReadLatency.Load()
	newAvg := (current + duration.Nanoseconds()) / 2
	c.avgReadLatency.Store(newAvg)
}

func (c *Collector) RecordWriteLatency(duration time.Duration) {
	current := c.avgWriteLatency.Load()
	newAvg := (current + duration.Nanoseconds()) / 2
	c.avgWriteLatency.Store(newAvg)
}

func (c *Collector) RecordSaveDuration(duration time.Duration) {
	c.mutex.Lock()
	c.lastSave = time.Now()
	c.saveDuration = duration
	c.mutex.Unlock()
}

func (c *Collector) UpdateDataSize(size int64) {
	c.dataSize.Store(size)
}

func (c *Collector) UpdateWALSize(size int64) {
	c.walSize.Store(size)
}

func (c *Collector) GetSnapshot() *Snapshot {
	c.mutex.Lock()
	lastSave := c.lastSave
	saveDuration := c.saveDuration
	c.mutex.Unlock()

	return &Snapshot{
		TotalReads:      c.totalReads.Load(),
		TotalWrites:     c.totalWrites.Load(),
		TotalDeletes:    c.totalDeletes.Load(),
		FailedOps:       c.failedOps.Load(),
		AvgReadLatency:  time.Duration(c.avgReadLatency.Load()),
		AvgWriteLatency: time.Duration(c.avgWriteLatency.Load()),
		DataSize:        c.dataSize.Load(),
		WALSize:         c.walSize.Load(),
		Uptime:          c.uptime,
		LastSave:        lastSave,
		SaveDuration:    saveDuration,
	}
}

func (c *Collector) Reset() {
	c.totalReads.Store(0)
	c.totalWrites.Store(0)
	c.totalDeletes.Store(0)
	c.failedOps.Store(0)
	c.avgReadLatency.Store(0)
	c.avgWriteLatency.Store(0)
}