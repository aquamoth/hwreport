package overview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"specreport/internal/report"
)

func TestGenerateUsesNewestSnapshotAndLoopsDetailNavigation(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "overview.html")
	detailDir := filepath.Join(tempDir, "hwreport-details")
	if err := os.MkdirAll(detailDir, 0o755); err != nil {
		t.Fatalf("create detail directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(detailDir, "stale.html"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale detail file: %v", err)
	}

	writeReportFile(t, tempDir, "alpha-2026-04-20.json", report.Report{
		SchemaVersion:  1,
		CollectedAtUTC: "2026-04-20T08:00:00Z",
		Hostname:       "alpha",
		Memory: report.Memory{
			TotalInstalledGB: float64Ptr(16),
		},
	})
	writeReportFile(t, tempDir, "alpha-2026-04-21.json", report.Report{
		SchemaVersion:  1,
		CollectedAtUTC: "2026-04-21T08:00:00Z",
		Hostname:       "alpha",
		Memory: report.Memory{
			TotalInstalledGB: float64Ptr(32),
		},
	})
	writeReportFile(t, tempDir, "beta-2026-04-21.json", report.Report{
		SchemaVersion:  1,
		CollectedAtUTC: "2026-04-21T09:00:00Z",
		Hostname:       "beta",
	})

	_, err := Generate(Options{
		InputDir:   tempDir,
		OutputPath: outputPath,
		Now:        time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
		Version:    "1.0.999",
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	overviewHTML := readFile(t, outputPath)
	if !strings.Contains(overviewHTML, "Generated 2026-04-21T12:00:00Z from 2 report(s)") {
		t.Fatalf("overview should render two latest rows, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "<button type=\"button\" class=\"sort-button\">Date</button>") {
		t.Fatalf("overview should include a date column, got:\n%s", overviewHTML)
	}
	if strings.Contains(overviewHTML, "<th>Report</th>") {
		t.Fatalf("overview should not include a report filename column, got:\n%s", overviewHTML)
	}
	if strings.Contains(overviewHTML, "Worst SMART") {
		t.Fatalf("overview should rename the SMART column, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "<button type=\"button\" class=\"sort-button\">SMART</button>") {
		t.Fatalf("overview should render sortable header buttons, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "const table = document.getElementById('overview-table');") {
		t.Fatalf("overview should include inline sorting script, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "<a href=\"hwreport-details/alpha-2026-04-21.html\">alpha</a>") {
		t.Fatalf("overview should link the computer name to newest alpha detail page, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "data-sort-value=\"2026-04-21 08:00 UTC\">2026-04-21 08:00 UTC</td>") {
		t.Fatalf("overview should show newest alpha report date, got:\n%s", overviewHTML)
	}
	if strings.Contains(overviewHTML, "alpha-2026-04-21.json</a></td>") {
		t.Fatalf("overview should not show the report filename as its own column, got:\n%s", overviewHTML)
	}
	if strings.Contains(overviewHTML, "alpha-2026-04-20.html") {
		t.Fatalf("overview should not link older alpha snapshot, got:\n%s", overviewHTML)
	}

	oldestDetail := readFile(t, filepath.Join(tempDir, "hwreport-details", "alpha-2026-04-20.html"))
	if !strings.Contains(oldestDetail, "Previous version: 2026-04-21 08:00 UTC - alpha-2026-04-21.json") {
		t.Fatalf("oldest detail should wrap previous link to newest snapshot, got:\n%s", oldestDetail)
	}
	if !strings.Contains(oldestDetail, "Next version: 2026-04-21 08:00 UTC - alpha-2026-04-21.json") {
		t.Fatalf("oldest detail should wrap next link to newest snapshot, got:\n%s", oldestDetail)
	}

	newestDetail := readFile(t, filepath.Join(tempDir, "hwreport-details", "alpha-2026-04-21.html"))
	if !strings.Contains(newestDetail, "Previous version: 2026-04-20 08:00 UTC - alpha-2026-04-20.json") {
		t.Fatalf("newest detail should point previous link to older snapshot, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "Next version: 2026-04-20 08:00 UTC - alpha-2026-04-20.json") {
		t.Fatalf("newest detail should wrap next link to oldest snapshot, got:\n%s", newestDetail)
	}
	if _, err := os.Stat(filepath.Join(detailDir, "stale.html")); !os.IsNotExist(err) {
		t.Fatalf("stale detail page should be removed before regeneration")
	}
}

func writeReportFile(t *testing.T, dir, name string, data report.Report) {
	t.Helper()

	payload, err := jsonMarshalIndent(data)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
		t.Fatalf("write report file: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(payload)
}

func float64Ptr(value float64) *float64 {
	return &value
}

func jsonMarshalIndent(data report.Report) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}
