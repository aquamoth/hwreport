package passmark

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDriveLookupRemembersMisses(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "drive-cache.json")
	client, err := NewDriveClient(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	// A loaded list: no network is needed for a model it doesn't contain.
	client.list, client.listLoaded = testDriveList, true

	if _, err := client.Lookup(context.Background(), "ST2000DM001-1ER164"); err == nil {
		t.Fatal("expected a miss")
	}

	// A new client reads the saved cache and must not look the drive up again:
	// with no list available it would otherwise fall back to the website.
	reloaded, err := NewDriveClient(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.listLoaded = true // list "unavailable", empty
	entry, ok := reloaded.cache.Entries[normalizeKey("ST2000DM001-1ER164")]
	if !ok || !entry.NotFound {
		t.Fatalf("miss was not cached: %+v", entry)
	}
	if _, err := reloaded.Lookup(context.Background(), "ST2000DM001-1ER164"); err == nil {
		t.Fatal("expected the cached miss")
	}

	if !recentlyCached(time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now()) {
		t.Error("a day-old miss should still be cached")
	}
	if recentlyCached(time.Now().Add(-31*24*time.Hour).UTC().Format(time.RFC3339), time.Now()) {
		t.Error("a 31-day-old miss should be retried")
	}
}
