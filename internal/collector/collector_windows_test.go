//go:build windows

package collector

import (
	"strings"
	"testing"
	"time"
)

func TestRunCollectorScriptStopsAtTimeout(t *testing.T) {
	saved := collectorTimeout
	collectorTimeout = 200 * time.Millisecond
	defer func() { collectorTimeout = saved }()

	started := time.Now()
	_, err := runCollectorScript()
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("expected a timeout error, got none")
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("timeout took %s to take effect", elapsed)
	}
}
