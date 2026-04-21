package normalize

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf16"
)

func StringPtr(s string) *string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func Float64Ptr(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func Uint32Ptr(v uint32) *uint32 {
	return &v
}

func BytesToGB(v uint64) *float64 {
	if v == 0 {
		return nil
	}
	gb := float64(v) / (1024 * 1024 * 1024)
	rounded := math.Round(gb*100) / 100
	return &rounded
}

func KBToGB(v uint64) *float64 {
	if v == 0 {
		return nil
	}
	gb := float64(v) / (1024 * 1024)
	rounded := math.Round(gb*100) / 100
	return &rounded
}

func MemoryTypeName(code uint64) *string {
	name := ""
	switch code {
	case 20:
		name = "DDR"
	case 21:
		name = "DDR2"
	case 24:
		name = "DDR3"
	case 26:
		name = "DDR4"
	case 34:
		name = "DDR5"
	}
	if name == "" {
		return nil
	}
	return &name
}

func DiskType(mediaType, model, description string) *string {
	joined := strings.ToLower(strings.Join([]string{mediaType, model, description}, " "))
	switch {
	case strings.Contains(joined, "ssd"), strings.Contains(joined, "solid state"), strings.Contains(joined, "nvme"), strings.Contains(joined, "emmc"):
		value := "ssd"
		return &value
	case strings.Contains(joined, "hdd"), strings.Contains(joined, "hard disk"):
		value := "hdd"
		return &value
	default:
		return nil
	}
}

func DecodeUint16String(values []uint16) *string {
	if len(values) == 0 {
		return nil
	}

	trimmed := values[:0]
	for _, value := range values {
		if value == 0 {
			continue
		}
		trimmed = append(trimmed, value)
	}

	if len(trimmed) == 0 {
		return nil
	}

	s := strings.TrimSpace(string(utf16.Decode(trimmed)))
	if s == "" {
		return nil
	}
	return &s
}

func DateOnlyFromCIM(value string) *string {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return nil
	}

	parsed, err := time.Parse("20060102", value[:8])
	if err != nil {
		return nil
	}

	out := parsed.Format("2006-01-02")
	return &out
}

func AggregateStrings(values []*string) *string {
	seen := map[string]struct{}{}
	var normalized []string
	for _, value := range values {
		if value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) != 1 {
		return nil
	}
	return &normalized[0]
}

func SlotLabel(deviceLocator, bankLabel string) *string {
	device := strings.TrimSpace(deviceLocator)
	bank := strings.TrimSpace(bankLabel)
	switch {
	case device != "" && bank != "":
		return StringPtr(fmt.Sprintf("%s (%s)", device, bank))
	case device != "":
		return StringPtr(device)
	default:
		return StringPtr(bank)
	}
}

func NormalizeKey(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range strings.ToUpper(value) {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func MatchKey(haystack, needle string) bool {
	if haystack == "" || needle == "" {
		return false
	}
	return strings.Contains(haystack, needle) || strings.Contains(needle, haystack)
}
