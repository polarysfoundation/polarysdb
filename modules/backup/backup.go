package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/logger"
)

type Config struct {
	BackupDir string
	Interval  time.Duration
	KeepCount int
}

type Manager struct {
	config     *Config
	logger     *logger.Logger
	lastBackup time.Time
}

type BackupSnapshotFunc func() ([]byte, error)

func NewManager(cfg *Config, log *logger.Logger) *Manager {
	return &Manager{
		config: cfg,
		logger: log,
	}
}

func (m *Manager) Start(ctx context.Context, snapshotFunc BackupSnapshotFunc) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.CreateBackup(snapshotFunc); err != nil {
				m.logger.Errorf("Backup failed: %v", err)
			}
		}
	}
}

func (m *Manager) CreateBackup(snapshotFunc BackupSnapshotFunc) error {
	m.logger.Info("Creating backup...")

	data, err := snapshotFunc()
	if err != nil {
		return fmt.Errorf("snapshot failed: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("backup_%s.db", timestamp)
	path := filepath.Join(m.config.BackupDir, filename)

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	m.lastBackup = time.Now()
	m.logger.Infof("Backup created: %s (%.2f MB)", filename, float64(len(data))/1024/1024)

	// Limpiar backups antiguos
	go m.cleanOldBackups()

	return nil
}

func (m *Manager) RestoreBackup(path string) ([]byte, error) {
	m.logger.Infof("Restoring from backup: %s", path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	m.logger.Infof("Restore completed (%.2f MB)", float64(len(data))/1024/1024)
	return data, nil
}

func (m *Manager) cleanOldBackups() {
	pattern := filepath.Join(m.config.BackupDir, "backup_*.db")
	files, err := filepath.Glob(pattern)
	if err != nil {
		m.logger.Errorf("Failed to list backups: %v", err)
		return
	}

	if len(files) <= m.config.KeepCount {
		return
	}

	// Ordenar por fecha (nombres tienen timestamp)
	sort.Strings(files)

	// Eliminar los más antiguos
	toDelete := len(files) - m.config.KeepCount
	for i := 0; i < toDelete; i++ {
		if err := os.Remove(files[i]); err != nil {
			m.logger.Warnf("Failed to delete old backup %s: %v", files[i], err)
		} else {
			m.logger.Infof("Deleted old backup: %s", filepath.Base(files[i]))
		}
	}
}

func (m *Manager) ListBackups() ([]string, error) {
	pattern := filepath.Join(m.config.BackupDir, "backup_*.db")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	
	// Retornar solo nombres de archivo
	result := make([]string, len(files))
	for i, f := range files {
		result[i] = filepath.Base(f)
	}

	return result, nil
}

func (m *Manager) GetLatestBackup() (string, error) {
	backups, err := m.ListBackups()
	if err != nil {
		return "", err
	}

	if len(backups) == 0 {
		return "", fmt.Errorf("no backups found")
	}

	return filepath.Join(m.config.BackupDir, backups[len(backups)-1]), nil
}

func (m *Manager) GetStats() map[string]any {
	backups, _ := m.ListBackups()
	return map[string]any{
		"total_backups": len(backups),
		"last_backup":   m.lastBackup,
		"backup_dir":    m.config.BackupDir,
	}
}