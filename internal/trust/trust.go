// Package trust persists fingerprints the user has marked "Always allow" so
// future PrepareReceive calls from those senders can skip the approval prompt.
package trust

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

const (
	appDirName  = "localsend-cli"
	trustedFile = "trusted.yaml"
)

type Entry struct {
	Fingerprint string    `yaml:"fingerprint"`
	Alias       string    `yaml:"alias,omitempty"`
	AddedAt     time.Time `yaml:"added_at"`
}

type Store struct {
	Entries []Entry `yaml:"trusted"`
}

var (
	mu    sync.RWMutex
	store Store
)

// Path returns the on-disk location of the trust file. Falls back to a
// hidden directory in the working directory when UserConfigDir is unavailable
// — same convention as config.ConfigPath.
func Path() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, appDirName, trustedFile)
	}
	return filepath.Join("."+appDirName, trustedFile)
}

// Load reads the trust file into memory. A missing file is not an error —
// first-time users have no trusted devices yet.
func Load() error {
	mu.Lock()
	defer mu.Unlock()
	return loadFrom(Path())
}

func loadFrom(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			store = Store{}
			return nil
		}
		return fmt.Errorf("read trust file: %w", err)
	}
	var s Store
	if err := yaml.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("parse trust file: %w", err)
	}
	store = s
	return nil
}

// IsTrusted reports whether the given fingerprint has been persisted via Add.
// Empty fingerprints are never trusted — callers should treat anonymous
// senders as untrusted.
func IsTrusted(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	for _, e := range store.Entries {
		if e.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

// Add records a fingerprint+alias pair and persists the store. Adding an
// already-trusted fingerprint is a no-op (the existing alias is preserved).
func Add(fingerprint, alias string) error {
	if fingerprint == "" {
		return errors.New("trust.Add: empty fingerprint")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, e := range store.Entries {
		if e.Fingerprint == fingerprint {
			return saveLocked(Path())
		}
	}
	store.Entries = append(store.Entries, Entry{
		Fingerprint: fingerprint,
		Alias:       alias,
		AddedAt:     time.Now().UTC(),
	})
	return saveLocked(Path())
}

// Forget removes any entry whose fingerprint equals query, whose fingerprint
// has query as a prefix (>=8 chars to avoid accidental wide matches), or
// whose alias equals query (case-insensitive). Returns the removed entries.
func Forget(query string) ([]Entry, error) {
	if query == "" {
		return nil, errors.New("trust.Forget: empty query")
	}
	mu.Lock()
	defer mu.Unlock()
	var removed []Entry
	kept := store.Entries[:0]
	for _, e := range store.Entries {
		if matches(e, query) {
			removed = append(removed, e)
			continue
		}
		kept = append(kept, e)
	}
	store.Entries = kept
	if len(removed) == 0 {
		return nil, nil
	}
	if err := saveLocked(Path()); err != nil {
		return removed, err
	}
	return removed, nil
}

// List returns a copy of all trusted entries — safe for callers to mutate.
func List() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Entry, len(store.Entries))
	copy(out, store.Entries)
	return out
}

func matches(e Entry, query string) bool {
	if e.Fingerprint == query {
		return true
	}
	if strings.EqualFold(e.Alias, query) {
		return true
	}
	if len(query) >= 8 && strings.HasPrefix(e.Fingerprint, query) {
		return true
	}
	return false
}

func saveLocked(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create trust dir: %w", err)
	}
	data, err := yaml.Marshal(store)
	if err != nil {
		return fmt.Errorf("marshal trust file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), trustedFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp trust file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp trust file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod temp trust file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp trust file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename trust file: %w", err)
	}
	return nil
}
