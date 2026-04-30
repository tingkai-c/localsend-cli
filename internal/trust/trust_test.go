package trust

import (
	"os"
	"path/filepath"
	"testing"
)

// reset clears the in-memory store and points the on-disk path at a temp dir.
// Returns the resolved trust file path so tests can inspect it directly.
// XDG_CONFIG_HOME is honored by os.UserConfigDir on Linux; on darwin/windows
// the path layout differs but the tests still exercise the round-trip — they
// just write under whatever UserConfigDir resolves to inside the temp HOME.
func reset(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("APPDATA", tmp) // windows fallback path inside UserConfigDir
	mu.Lock()
	store = Store{}
	mu.Unlock()
	return Path()
}

func TestLoadMissingIsNotError(t *testing.T) {
	reset(t)
	if err := Load(); err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if got := List(); len(got) != 0 {
		t.Fatalf("expected empty store, got %d entries", len(got))
	}
}

func TestAddPersistsAcrossLoad(t *testing.T) {
	path := reset(t)
	if err := Add("fp-abc", "Alice Phone"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected trust file at %s: %v", path, err)
	}
	mu.Lock()
	store = Store{}
	mu.Unlock()
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !IsTrusted("fp-abc") {
		t.Fatalf("expected fp-abc to be trusted after reload")
	}
}

func TestAddIdempotent(t *testing.T) {
	reset(t)
	for i := 0; i < 3; i++ {
		if err := Add("fp-dup", "Bob"); err != nil {
			t.Fatalf("Add iter %d: %v", i, err)
		}
	}
	if got := List(); len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestIsTrustedEmptyRejected(t *testing.T) {
	reset(t)
	if IsTrusted("") {
		t.Fatalf("empty fingerprint must not be trusted")
	}
}

func TestForgetByAlias(t *testing.T) {
	reset(t)
	_ = Add("fp-1", "Alice")
	_ = Add("fp-2", "Bob")
	removed, err := Forget("alice") // case-insensitive
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(removed) != 1 || removed[0].Fingerprint != "fp-1" {
		t.Fatalf("expected to remove fp-1, got %+v", removed)
	}
	if IsTrusted("fp-1") {
		t.Fatalf("fp-1 should be gone")
	}
	if !IsTrusted("fp-2") {
		t.Fatalf("fp-2 should remain")
	}
}

func TestForgetByFingerprintPrefix(t *testing.T) {
	reset(t)
	_ = Add("abcdefghijkl", "Alice")
	_ = Add("zzzzzzzzzzzz", "Bob")
	removed, err := Forget("abcdefgh") // 8-char prefix
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
}

func TestForgetShortPrefixDoesNotMatch(t *testing.T) {
	reset(t)
	_ = Add("abcdefghijkl", "Alice")
	removed, err := Forget("abc") // <8 chars: only exact alias / fingerprint match
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("short prefixes must not match, got %+v", removed)
	}
}

func TestForgetMissingNoOp(t *testing.T) {
	reset(t)
	_ = Add("fp-x", "X")
	removed, err := Forget("not-there")
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removals, got %+v", removed)
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	reset(t)
	if err := Add("fp-mkdir", "Test"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	parent := filepath.Dir(Path())
	if _, err := os.Stat(parent); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	}
}
