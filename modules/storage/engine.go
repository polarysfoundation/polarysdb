package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/polarysfoundation/polarysdb/modules/common"
	"github.com/polarysfoundation/polarysdb/modules/crypto"
	"golang.org/x/sys/unix"
)

type Config struct {
	DataPath      string
	EncryptionKey common.Key
	Compression   bool
	MaxRetries    int
	RetryDelay    time.Duration
}

type Engine struct {
	path        string
	key         common.Key
	compression bool
	maxRetries  int
	retryDelay  time.Duration
	mutex       sync.Mutex
}

func New(cfg *Config) (*Engine, error) {
	return &Engine{
		path:        cfg.DataPath,
		key:         cfg.EncryptionKey,
		compression: cfg.Compression,
		maxRetries:  cfg.MaxRetries,
		retryDelay:  cfg.RetryDelay,
	}, nil
}

func (e *Engine) Save(data map[string]map[string]any) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	// TODO: Aplicar compresión si está habilitada
	
	encryptedData, err := crypto.Encrypt(jsonData, e.key)
	if err != nil {
		return fmt.Errorf("encryption error: %w", err)
	}

	return e.atomicWrite(encryptedData)
}

func (e *Engine) atomicWrite(data []byte) error {
	tmp := e.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)

	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}

	return os.Rename(tmp, e.path)
}

func (e *Engine) Load() (map[string]map[string]any, time.Time, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	info, err := os.Stat(e.path)
	if os.IsNotExist(err) {
		return make(map[string]map[string]any), time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}

	f, err := os.Open(e.path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()

	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH); err != nil {
		return nil, time.Time{}, err
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)

	encryptedData, err := io.ReadAll(f)
	if err != nil {
		return nil, time.Time{}, err
	}

	decryptedData, err := crypto.Decrypt(encryptedData, e.key)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("decryption error: %w", err)
	}

	var data map[string]map[string]any
	if err := json.Unmarshal(decryptedData, &data); err != nil {
		return nil, time.Time{}, fmt.Errorf("unmarshal error: %w", err)
	}

	return data, info.ModTime(), nil
}

func (e *Engine) Serialize(data map[string]map[string]any) ([]byte, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return crypto.Encrypt(jsonData, e.key)
}

func (e *Engine) ExportPlain(data map[string]map[string]any, path string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, jsonData, 0600)
}

func (e *Engine) ImportPlain(path string) (map[string]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result map[string]map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}

func (e *Engine) ExportEncrypted(data map[string]map[string]any, path string) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	encryptedData, err := crypto.Encrypt(jsonData, e.key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encryptedData, 0600)
}

func (e *Engine) ImportEncrypted(path string) (map[string]map[string]any, error) {
	encryptedData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decryptedData, err := crypto.Decrypt(encryptedData, e.key)
	if err != nil {
		return nil, err
	}
	var result map[string]map[string]any
	err = json.Unmarshal(decryptedData, &result)
	return result, err
}

func (e *Engine) UpdateKey(newKey common.Key) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.key = newKey
}

func (e *Engine) GetPath() string {
	return e.path
}