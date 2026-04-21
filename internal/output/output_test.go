package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultBaseFilename(t *testing.T) {
	got := DefaultBaseFilename(`pc:name`, time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC))
	if got != "pc-name-2026-04-21.json" {
		t.Fatalf("unexpected base filename: %s", got)
	}
}

func TestUniquePathIndexesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "host-2026-04-21.json")

	if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	got, err := UniquePath(target)
	if err != nil {
		t.Fatalf("UniquePath: %v", err)
	}

	expected := filepath.Join(dir, "host-2026-04-21-1.json")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestResolvePathUsesDirectoryWhenOutIsDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolvePath(dir, "office-pc", time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}

	expected := filepath.Join(dir, "office-pc-2026-04-21.json")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
