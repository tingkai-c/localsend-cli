package handlers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileIDForPathUsesBaseForSingleFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "message.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := fileIDForPath(file, file)
	if err != nil {
		t.Fatalf("fileIDForPath() error = %v", err)
	}
	if id != "message.txt" {
		t.Fatalf("id = %q, want message.txt", id)
	}
}

func TestFileIDForPathPreservesDirectoryRelativePath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "nested", "report.txt")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := fileIDForPath(root, file)
	if err != nil {
		t.Fatalf("fileIDForPath() error = %v", err)
	}
	if id != "nested/report.txt" {
		t.Fatalf("id = %q, want nested/report.txt", id)
	}
}

func TestSafeOutputPathAllowsNestedRelativePath(t *testing.T) {
	root := t.TempDir()
	got, err := safeOutputPath(root, "nested/report.txt")
	if err != nil {
		t.Fatalf("safeOutputPath() error = %v", err)
	}
	want := filepath.Join(root, "nested", "report.txt")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestSafeOutputPathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := safeOutputPath(root, "../escape.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
	if _, err := safeOutputPath(root, filepath.Join(string(filepath.Separator), "tmp", "escape.txt")); err == nil {
		t.Fatalf("expected absolute path to be rejected")
	}
}
