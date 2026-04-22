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
			Type:             stringPtr("DDR4"),
			TotalInstalledGB: float64Ptr(16),
		},
	})
	writeReportFile(t, tempDir, "alpha-2026-04-21.json", report.Report{
		SchemaVersion:  1,
		CollectedAtUTC: "2026-04-21T08:00:00Z",
		Hostname:       "alpha",
		Memory: report.Memory{
			Type:               stringPtr("DDR5"),
			ConfiguredSpeedMHz: intPtr(5600),
			RatedSpeedMHz:      intPtr(5600),
			TotalInstalledGB:   float64Ptr(32),
			Modules: []report.MemoryModule{
				{SizeGB: float64Ptr(16)},
				{SizeGB: float64Ptr(16)},
			},
		},
		Storage: []report.Drive{
			{
				Model:       stringPtr("Samsung SSD 990 PRO 2TB"),
				Type:        stringPtr("ssd"),
				SizeGB:      float64Ptr(1907.73),
				SmartStatus: stringPtr("ok"),
				Benchmark: &report.DriveBenchmark{
					CanonicalName:       stringPtr("Samsung SSD 990 PRO 2TB"),
					DriveMark:           intPtr(60345),
					SequentialReadMBps:  float64Ptr(4947),
					SequentialWriteMBps: float64Ptr(4642),
					IOPS4KQD1MBps:       float64Ptr(82),
					LookupURL:           stringPtr("https://www.harddrivebenchmark.net/hdd.php?hdd=Samsung+SSD+990+PRO+2TB&id=99999"),
				},
			},
		},
		Monitors: []report.Monitor{
			{
				Manufacturer:   stringPtr("Dell"),
				Model:          stringPtr("U2422H"),
				PixelWidth:     uint32Ptr(1920),
				PixelHeight:    uint32Ptr(1080),
				PhysicalWidth:  float64Ptr(53),
				PhysicalHeight: float64Ptr(30),
				PhysicalUnit:   stringPtr("cm"),
			},
		},
	})
	writeReportFile(t, tempDir, "beta-2026-04-21.json", report.Report{
		SchemaVersion:  1,
		CollectedAtUTC: "2026-04-21T09:00:00Z",
		Hostname:       "beta",
		Storage: []report.Drive{
			{
				Model:       stringPtr("Some Offline Test Drive"),
				Type:        stringPtr("ssd"),
				SizeGB:      float64Ptr(512),
				SmartStatus: stringPtr("ok"),
			},
		},
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
	if !strings.Contains(overviewHTML, "<button type=\"button\" class=\"sort-button\">RAM Type</button>") {
		t.Fatalf("overview should include a RAM type column, got:\n%s", overviewHTML)
	}
	if !strings.Contains(overviewHTML, "data-sort-value=\"DDR5\">DDR5</td>") {
		t.Fatalf("overview should render the memory type for the latest alpha snapshot, got:\n%s", overviewHTML)
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
	if !strings.Contains(newestDetail, "<tr><th>Type</th><td>DDR5</td></tr>") {
		t.Fatalf("newest detail should render the memory type, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "<tr><th>Memory</th><td>2 x 16 GB DDR5</td></tr>") {
		t.Fatalf("newest detail should render the one-line memory summary in the system section, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "<tr><th>Configured Speed</th><td>5600 MHz</td></tr>") {
		t.Fatalf("newest detail should render configured memory speed, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "<tr><th>Rated Speed</th><td>5600 MHz</td></tr>") {
		t.Fatalf("newest detail should render rated memory speed, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "Samsung SSD 990 PRO 2TB") || !strings.Contains(newestDetail, ">60345<") {
		t.Fatalf("newest detail should render drive benchmark data, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, ">82 MB/s<") {
		t.Fatalf("newest detail should render IOPS with its current display unit, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, "Hard Drive Benchmark</a>") {
		t.Fatalf("newest detail should link to the drive benchmark source, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, ">24&#34;<") {
		t.Fatalf("newest detail should render monitor diagonal in inches, got:\n%s", newestDetail)
	}
	if !strings.Contains(newestDetail, ">16:9<") {
		t.Fatalf("newest detail should render monitor aspect ratio, got:\n%s", newestDetail)
	}
	if _, err := os.Stat(filepath.Join(detailDir, "stale.html")); !os.IsNotExist(err) {
		t.Fatalf("stale detail page should be removed before regeneration")
	}

	betaDetail := readFile(t, filepath.Join(tempDir, "hwreport-details", "beta-2026-04-21.html"))
	if strings.Contains(betaDetail, "\"benchmark\"") {
		t.Fatalf("raw JSON should remain the original source report without injected benchmark data, got:\n%s", betaDetail)
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

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func uint32Ptr(value uint32) *uint32 {
	return &value
}

func jsonMarshalIndent(data report.Report) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}
