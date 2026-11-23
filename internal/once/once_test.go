package once

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoneIsFalseUntilMarked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hwreport")

	done, err := Done(dir, "inventory-2026-11")
	if err != nil || done {
		t.Fatalf("Done before Mark = %v, %v; want false, nil", done, err)
	}

	if err := Mark(dir, "inventory-2026-11", `\\srv\share\PC-2026-11-23.json`, time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	done, err = Done(dir, "inventory-2026-11")
	if err != nil || !done {
		t.Fatalf("Done after Mark = %v, %v; want true, nil", done, err)
	}

	content, err := os.ReadFile(MarkerPath(dir, "inventory-2026-11"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "2026-11-23T09:00:00Z") || !strings.Contains(string(content), "PC-2026-11-23.json") {
		t.Fatalf("marker content = %q", content)
	}
}

func TestKeysAreIndependent(t *testing.T) {
	dir := t.TempDir()
	if err := Mark(dir, "inventory-2026-11", "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	done, err := Done(dir, "inventory-2027-05")
	if err != nil || done {
		t.Fatalf("other key Done = %v, %v; want false, nil", done, err)
	}
}

func TestValidateKey(t *testing.T) {
	for _, ok := range []string{"inventory-2026-11", "a", "v1.2_x"} {
		if err := ValidateKey(ok); err != nil {
			t.Errorf("ValidateKey(%q) = %v; want nil", ok, err)
		}
	}
	for _, bad := range []string{"", `..\evil`, "a/b", "has space", strings.Repeat("x", 65)} {
		if err := ValidateKey(bad); err == nil {
			t.Errorf("ValidateKey(%q) = nil; want error", bad)
		}
	}
}
