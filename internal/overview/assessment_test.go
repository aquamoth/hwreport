package overview

import (
	"strings"
	"testing"
	"time"

	"specreport/internal/report"
)

func floatPtr(v float64) *float64 { return &v }
func intValuePtr(v int) *int      { return &v }

var assessNow = time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)

func memoryReport(gb float64) *report.Report {
	return &report.Report{Memory: report.Memory{TotalInstalledGB: floatPtr(gb)}}
}

func findingTexts(a assessment) string {
	return a.Summary()
}

func TestAssessHealthyComputer(t *testing.T) {
	drives := []detailDrive{{
		Model: "WD BLACK SN7100 1TB", Tier: "NVMe SSD", SmartStatus: "ok",
		Volumes: []detailVolume{{DriveLetter: "C:", SizeGB: floatPtr(930), FreeGB: floatPtr(600)}},
	}}
	got := assess(memoryReport(32), drives, intValuePtr(30000), "Q1 2025", assessNow)
	if got.Level != levelOK || len(got.Findings) != 0 {
		t.Fatalf("expected OK without findings, got %s: %q", got.Label(), findingTexts(got))
	}
}

func TestAssessOldSlowComputer(t *testing.T) {
	drives := []detailDrive{{
		Model: "Seagate ST1000DM010", Tier: "HDD", SmartStatus: "ok",
		Volumes: []detailVolume{{DriveLetter: "C:", SizeGB: floatPtr(930), FreeGB: floatPtr(80)}},
	}}
	got := assess(memoryReport(4), drives, intValuePtr(4929), "Q2 2014", assessNow)
	if got.Level != levelWarning {
		t.Fatalf("expected Warning, got %s", got.Label())
	}
	for _, want := range []string{"Windows runs from a hard disk", "C: is low on space", "Only 4 GB memory", "Slow CPU", "CPU from Q2 2014, about 12 years old"} {
		if !strings.Contains(findingTexts(got), want) {
			t.Errorf("missing finding %q in %q", want, findingTexts(got))
		}
	}
}

func TestAssessCriticalFindings(t *testing.T) {
	drives := []detailDrive{
		{Model: "Samsung SSD 980", Tier: "NVMe SSD", SmartStatus: "ok",
			Volumes: []detailVolume{{DriveLetter: "C:", SizeGB: floatPtr(465), FreeGB: floatPtr(8)}}},
		{Model: "WDC WD10EZEX", Tier: "HDD", SmartStatus: "warning"},
	}
	got := assess(memoryReport(16), drives, nil, "", assessNow)
	if got.Level != levelCritical {
		t.Fatalf("expected Critical, got %s", got.Label())
	}
	if got.Findings[0].Level != levelCritical {
		t.Errorf("findings should be ordered worst first: %q", findingTexts(got))
	}
	for _, want := range []string{"WDC WD10EZEX reports a SMART warning", "C: is almost full", "Has a hard disk for data"} {
		if !strings.Contains(findingTexts(got), want) {
			t.Errorf("missing finding %q in %q", want, findingTexts(got))
		}
	}
	if got.SortValue() <= (assessment{Level: levelWarning, Findings: make([]finding, 9)}).SortValue() {
		t.Errorf("a critical computer must sort above any warning-level computer")
	}
}

func TestAssessReportWithoutVolumes(t *testing.T) {
	// Schema 3 reports don't say which drive holds Windows.
	onlyHDD := []detailDrive{{Model: "WDC WD10EZEX", Tier: "HDD", SmartStatus: "ok"}}
	if got := assess(memoryReport(16), onlyHDD, nil, "", assessNow); !strings.Contains(findingTexts(got), "Windows runs from a hard disk") {
		t.Errorf("all-HDD computer should warn, got %q", findingTexts(got))
	}
	mixed := []detailDrive{{Model: "SSD", Tier: "SATA SSD", SmartStatus: "ok"}, {Model: "HDD", Tier: "HDD", SmartStatus: "ok"}}
	if got := assess(memoryReport(16), mixed, nil, "", assessNow); got.Level != levelNotice {
		t.Errorf("mixed computer should be a notice, got %s %q", got.Label(), findingTexts(got))
	}
}

func TestDriveTier(t *testing.T) {
	cases := []struct {
		driveType, bus string
		mark           *int
		want           string
	}{
		{"hdd", "SATA", intValuePtr(5478), "HDD"},
		{"ssd", "NVMe", nil, "NVMe SSD"},
		{"ssd", "SATA", intValuePtr(3455), "SATA SSD"},
		{"ssd", "", intValuePtr(46894), "NVMe SSD (est.)"},
		{"ssd", "", intValuePtr(3455), "SATA SSD (est.)"},
		{"ssd", "", nil, "SSD"},
	}
	for _, tc := range cases {
		if got := driveTier(tc.driveType, tc.bus, tc.mark); got != tc.want {
			t.Errorf("driveTier(%q, %q) = %q, want %q", tc.driveType, tc.bus, got, tc.want)
		}
	}
	if got := speedClass("HDD", intValuePtr(5478)); got != "slow" {
		t.Errorf("a hard disk is always slow, got %q", got)
	}
}

func TestDescribeAges(t *testing.T) {
	if got := describeFirstUse("2019-05-02", assessNow); got != "2019-05-02 (about 7 years ago)" {
		t.Errorf("describeFirstUse = %q", got)
	}
	if got := describeCPUFirstSeen("Q2 2014", assessNow); got != "Q2 2014 (about 12 years ago)" {
		t.Errorf("describeCPUFirstSeen = %q", got)
	}
	if got := formatVolumes([]detailVolume{{DriveLetter: "C:", SizeGB: floatPtr(200), FreeGB: floatPtr(50)}}); len(got) != 1 || got[0] != "C: 50 GB free of 200 GB (25%)" {
		t.Errorf("formatVolumes = %q", got)
	}
}
