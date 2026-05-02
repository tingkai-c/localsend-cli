package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store := NewStore(filepath.Join(t.TempDir(), "history.json"))
	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return fixed }
	return store
}

func TestListMissingFileIsEmpty(t *testing.T) {
	records, err := testStore(t).List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected empty history, got %d records", len(records))
	}
}

func TestAddPersistsAndFillsFields(t *testing.T) {
	store := testStore(t)
	added, err := store.Add(Record{Direction: DirectionReceived, Status: StatusCompleted, FileName: "report.pdf", Size: 42})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if added.ID == "" || added.StartedAt.IsZero() || added.CompletedAt.IsZero() {
		t.Fatalf("Add() did not fill generated fields: %+v", added)
	}
	records, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(records) != 1 || records[0].FileName != "report.pdf" || records[0].Size != 42 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestListNewestFirst(t *testing.T) {
	store := testStore(t)
	older := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	if _, err := store.Add(Record{ID: "old", Direction: DirectionSent, Status: StatusCompleted, FileName: "old", CompletedAt: older}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(Record{ID: "new", Direction: DirectionSent, Status: StatusCompleted, FileName: "new", CompletedAt: newer}); err != nil {
		t.Fatal(err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if got := records[0].ID; got != "new" {
		t.Fatalf("first record = %q, want new", got)
	}
}

func TestDeleteAndClear(t *testing.T) {
	store := testStore(t)
	first, _ := store.Add(Record{Direction: DirectionReceived, Status: StatusCompleted, FileName: "a"})
	_, _ = store.Add(Record{Direction: DirectionReceived, Status: StatusCompleted, FileName: "b"})
	removed, err := store.Delete(first.ID)
	if err != nil || !removed {
		t.Fatalf("Delete() removed=%v err=%v", removed, err)
	}
	records, _ := store.List()
	if len(records) != 1 || records[0].FileName != "b" {
		t.Fatalf("unexpected records after delete: %+v", records)
	}
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	records, _ = store.List()
	if len(records) != 0 {
		t.Fatalf("expected clear history, got %+v", records)
	}
}

func TestCorruptFileReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(path).List()
	if err == nil {
		t.Fatalf("expected parse error")
	}
}
