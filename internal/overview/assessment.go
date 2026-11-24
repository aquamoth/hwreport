package overview

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"specreport/internal/report"
)

// Finding levels, also the order of the Status column.
const (
	levelOK       = 0
	levelNotice   = 1
	levelWarning  = 2
	levelCritical = 3
)

// Thresholds for the findings. Kept together so they are easy to adjust.
const (
	systemFreeCriticalGB   = 10.0
	systemFreeCriticalPct  = 5.0
	systemFreeWarningGB    = 30.0
	systemFreeWarningPct   = 10.0
	memoryWarningBelowGB   = 8.0
	memoryNoticeBelowGB    = 16.0
	cpuMarkWarningBelow    = 6000
	cpuMarkNoticeBelow     = 10000
	ageWarningYears        = 8.0
	ageNoticeYears         = 6.0
	nvmeClassDriveMarkFrom = 10000 // SATA SSDs top out around 5,000-6,000
)

type finding struct {
	Level int
	Text  string
}

type assessment struct {
	Level    int
	Findings []finding
}

func (a assessment) Label() string {
	switch a.Level {
	case levelCritical:
		return "Critical"
	case levelWarning:
		return "Warning"
	case levelNotice:
		return "Notice"
	default:
		return "OK"
	}
}

func (a assessment) BadgeClass() string {
	switch a.Level {
	case levelCritical:
		return "smart-error"
	case levelWarning:
		return "smart-warning"
	case levelNotice:
		return "smart-unknown"
	default:
		return "smart-ok"
	}
}

// SortValue orders by the worst level, then by how many findings there are.
func (a assessment) SortValue() int {
	return a.Level*100 + len(a.Findings)
}

func (a assessment) Summary() string {
	texts := make([]string, 0, len(a.Findings))
	for _, item := range a.Findings {
		texts = append(texts, item.Text)
	}
	return strings.Join(texts, "\n")
}

// driveTier names the kind of drive: from the bus type when the report has it
// (schema 4), otherwise estimated from the PassMark Drive Mark.
func driveTier(driveType, busType string, driveMark *int) string {
	driveType = strings.ToLower(strings.TrimSpace(driveType))
	bus := strings.ToUpper(strings.TrimSpace(busType))
	if driveType == "hdd" {
		return "HDD"
	}
	switch bus {
	case "NVME":
		return "NVMe SSD"
	case "SATA", "ATA":
		if driveType == "ssd" {
			return "SATA SSD"
		}
	case "USB":
		return "USB"
	}
	if driveType == "ssd" && driveMark != nil {
		if *driveMark >= nvmeClassDriveMarkFrom {
			return "NVMe SSD (est.)"
		}
		return "SATA SSD (est.)"
	}
	if driveType == "ssd" {
		return "SSD"
	}
	return ""
}

// speedClass gives a rough speed class from the PassMark Drive Mark. Hard disks
// are always slow, whatever their mark says.
func speedClass(tier string, driveMark *int) string {
	if tier == "HDD" {
		return "slow"
	}
	if driveMark == nil {
		return ""
	}
	switch mark := *driveMark; {
	case mark < 2000:
		return "slow"
	case mark < 10000:
		return "basic"
	case mark < 30000:
		return "fast"
	default:
		return "very fast"
	}
}

var quarterYearRegexp = regexp.MustCompile(`Q([1-4])\s+(\d{4})`)

// parseQuarter turns PassMark's "Q2 2014" into the middle of that quarter.
func parseQuarter(value string) (time.Time, bool) {
	m := quarterYearRegexp.FindStringSubmatch(value)
	if m == nil {
		return time.Time{}, false
	}
	quarter, _ := strconv.Atoi(m[1])
	year, _ := strconv.Atoi(m[2])
	return time.Date(year, time.Month(quarter*3-1), 15, 0, 0, 0, 0, time.UTC), true
}

func yearsBetween(from, to time.Time) float64 {
	return to.Sub(from).Hours() / 24 / 365.25
}

func formatAge(years float64) string {
	if years < 1 {
		return "less than a year"
	}
	return fmt.Sprintf("%.0f years", math.Floor(years))
}

// describeFirstUse: "2019-05-02 (about 7 years ago)".
func describeFirstUse(date string, now time.Time) string {
	date = strings.TrimSpace(date)
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return fmt.Sprintf("%s (%s ago)", date, aboutAge(yearsBetween(parsed, now)))
}

// describeCPUFirstSeen: "Q2 2014 (about 12 years ago)".
func describeCPUFirstSeen(firstSeen string, now time.Time) string {
	released, ok := parseQuarter(firstSeen)
	if !ok {
		return strings.TrimSpace(firstSeen)
	}
	return fmt.Sprintf("%s (%s ago)", firstSeen, aboutAge(yearsBetween(released, now)))
}

func aboutAge(years float64) string {
	if years < 1 {
		return "less than a year"
	}
	return "about " + formatAge(years)
}

func formatVolumes(volumes []detailVolume) []string {
	lines := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		if volume.SizeGB == nil || volume.FreeGB == nil || *volume.SizeGB <= 0 {
			lines = append(lines, volume.DriveLetter)
			continue
		}
		pct := *volume.FreeGB / *volume.SizeGB * 100
		lines = append(lines, fmt.Sprintf("%s %s GB free of %s GB (%.0f%%)", volume.DriveLetter, trimFloat(*volume.FreeGB), trimFloat(*volume.SizeGB), pct))
	}
	return lines
}

func assess(data *report.Report, drives []detailDrive, cpuMark *int, cpuFirstSeen string, now time.Time) assessment {
	var findings []finding
	add := func(level int, format string, args ...any) {
		findings = append(findings, finding{Level: level, Text: fmt.Sprintf(format, args...)})
	}

	// Drive health.
	hddCount := 0
	systemIndex := -1
	var systemVolume *detailVolume
	for idx, drive := range drives {
		switch strings.ToLower(drive.SmartStatus) {
		case "error":
			add(levelCritical, "Drive %s reports a SMART error", drive.Model)
		case "warning":
			add(levelCritical, "Drive %s reports a SMART warning", drive.Model)
		}
		if drive.Tier == "HDD" {
			hddCount++
		}
		for vIdx := range drive.Volumes {
			if strings.EqualFold(drive.Volumes[vIdx].DriveLetter, "C:") && systemIndex < 0 {
				systemIndex = idx
				systemVolume = &drives[idx].Volumes[vIdx]
			}
		}
	}

	// The drive Windows runs from. Reports before schema 4 don't say which it is.
	switch {
	case systemIndex >= 0 && drives[systemIndex].Tier == "HDD":
		add(levelWarning, "Windows runs from a hard disk (%s)", drives[systemIndex].Model)
	case systemIndex >= 0 && hddCount > 0:
		add(levelNotice, "Has a hard disk for data")
	case systemIndex < 0 && hddCount > 0 && hddCount == len(drives):
		add(levelWarning, "Windows runs from a hard disk")
	case systemIndex < 0 && hddCount > 0:
		add(levelNotice, "Has a hard disk")
	}
	if systemVolume != nil && systemVolume.SizeGB != nil && systemVolume.FreeGB != nil && *systemVolume.SizeGB > 0 {
		free, size := *systemVolume.FreeGB, *systemVolume.SizeGB
		pct := free / size * 100
		switch {
		case free < systemFreeCriticalGB || pct < systemFreeCriticalPct:
			add(levelCritical, "C: is almost full: %s GB free of %s GB", trimFloat(free), trimFloat(size))
		case free < systemFreeWarningGB || pct < systemFreeWarningPct:
			add(levelWarning, "C: is low on space: %s GB free of %s GB", trimFloat(free), trimFloat(size))
		}
	}

	// Memory.
	if gb := data.Memory.TotalInstalledGB; gb != nil && *gb > 0 {
		switch {
		case *gb < memoryWarningBelowGB:
			add(levelWarning, "Only %s GB memory", trimFloat(*gb))
		case *gb < memoryNoticeBelowGB:
			add(levelNotice, "%s GB memory", trimFloat(*gb))
		}
	}

	// CPU speed.
	if cpuMark != nil {
		switch {
		case *cpuMark < cpuMarkWarningBelow:
			add(levelWarning, "Slow CPU (CPU Mark %d)", *cpuMark)
		case *cpuMark < cpuMarkNoticeBelow:
			add(levelNotice, "Modest CPU (CPU Mark %d)", *cpuMark)
		}
	}

	// Age: when the CPU came on the market; the hardware can't be older.
	if released, ok := parseQuarter(cpuFirstSeen); ok {
		years := yearsBetween(released, now)
		switch {
		case years >= ageWarningYears:
			add(levelWarning, "CPU from %s, about %s old", cpuFirstSeen, formatAge(years))
		case years >= ageNoticeYears:
			add(levelNotice, "CPU from %s, about %s old", cpuFirstSeen, formatAge(years))
		}
	}

	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Level > findings[j].Level })
	result := assessment{Findings: findings}
	for _, item := range findings {
		if item.Level > result.Level {
			result.Level = item.Level
		}
	}
	return result
}
