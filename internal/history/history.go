// Package history persists LocalSend transfer history for both TUI and
// headless/CLI adapters. It intentionally stores plain JSON so future TUI
// screens can read the same records without pulling in UI dependencies.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	appDirName  = "localsend-cli"
	historyFile = "history.json"

	DirectionSent     = "sent"
	DirectionReceived = "received"

	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

// Record is one persisted transfer-history entry.
type Record struct {
	ID              string    `json:"id"`
	Direction       string    `json:"direction"`
	Status          string    `json:"status"`
	FileName        string    `json:"fileName"`
	Path            string    `json:"path,omitempty"`
	Size            int64     `json:"size"`
	PeerAlias       string    `json:"peerAlias,omitempty"`
	PeerIP          string    `json:"peerIp,omitempty"`
	PeerFingerprint string    `json:"peerFingerprint,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	CompletedAt     time.Time `json:"completedAt"`
	Error           string    `json:"error,omitempty"`
}

type fileData struct {
	Records []Record `json:"records"`
}

// Store owns history persistence at one path.
type Store struct {
	path string
	now  func() time.Time
}

// Path returns the default transfer-history path.
func Path() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, appDirName, historyFile)
	}
	return filepath.Join("."+appDirName, historyFile)
}

// NewStore creates a store at path. Tests should use temp paths here.
func NewStore(path string) *Store {
	return &Store{path: path, now: func() time.Time { return time.Now().UTC() }}
}

// DefaultStore returns a store at the standard app history path.
func DefaultStore() *Store { return NewStore(Path()) }

// Add appends a record and persists it. Missing timestamps and IDs are filled.
func (s *Store) Add(record Record) (Record, error) {
	if s == nil {
		return Record{}, errors.New("history: nil store")
	}
	data, err := s.load()
	if err != nil {
		return Record{}, err
	}
	now := s.now()
	if record.StartedAt.IsZero() {
		record.StartedAt = now
	}
	if record.CompletedAt.IsZero() {
		record.CompletedAt = now
	}
	if record.ID == "" {
		record.ID = fmt.Sprintf("%d-%s-%s", record.CompletedAt.UnixNano(), record.Direction, safeBase(record.FileName))
	}
	data.Records = append(data.Records, record)
	return record, s.save(data)
}

// List returns records in reverse chronological order by completion time.
func (s *Store) List() ([]Record, error) {
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	records := append([]Record(nil), data.Records...)
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].CompletedAt.After(records[j].CompletedAt)
	})
	return records, nil
}

// Delete removes a record by ID. It returns true when a record was removed.
func (s *Store) Delete(id string) (bool, error) {
	if id == "" {
		return false, errors.New("history: empty id")
	}
	data, err := s.load()
	if err != nil {
		return false, err
	}
	kept := data.Records[:0]
	removed := false
	for _, record := range data.Records {
		if record.ID == id {
			removed = true
			continue
		}
		kept = append(kept, record)
	}
	data.Records = kept
	if !removed {
		return false, nil
	}
	return true, s.save(data)
}

// Clear removes all history records.
func (s *Store) Clear() error {
	if s == nil {
		return errors.New("history: nil store")
	}
	return s.save(fileData{})
}

func (s *Store) load() (fileData, error) {
	if s == nil {
		return fileData{}, errors.New("history: nil store")
	}
	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileData{}, nil
		}
		return fileData{}, fmt.Errorf("read history file: %w", err)
	}
	if len(bytes) == 0 {
		return fileData{}, nil
	}
	var data fileData
	if err := json.Unmarshal(bytes, &data); err != nil {
		return fileData{}, fmt.Errorf("parse history file: %w", err)
	}
	return data, nil
}

func (s *Store) save(data fileData) error {
	if data.Records == nil {
		data.Records = []Record{}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), historyFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("create history temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(bytes); err != nil {
		tmp.Close()
		return fmt.Errorf("write history temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close history temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace history file: %w", err)
	}
	return nil
}

func safeBase(name string) string {
	base := filepath.Base(name)
	if base == "." || base == string(filepath.Separator) {
		return "transfer"
	}
	return base
}

// Convenience wrappers for the default store.
func Add(record Record) (Record, error) { return DefaultStore().Add(record) }
func List() ([]Record, error)           { return DefaultStore().List() }
func Delete(id string) (bool, error)    { return DefaultStore().Delete(id) }
func Clear() error                      { return DefaultStore().Clear() }
