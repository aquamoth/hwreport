package output

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ResolvePath(outFlag, hostname string, now time.Time) (string, error) {
	baseName := DefaultBaseFilename(hostname, now)

	switch {
	case strings.TrimSpace(outFlag) == "":
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return UniquePath(filepath.Join(cwd, baseName))
	default:
		cleaned := filepath.Clean(outFlag)
		info, err := os.Stat(cleaned)
		if err == nil && info.IsDir() {
			return UniquePath(filepath.Join(cleaned, baseName))
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return UniquePath(cleaned)
	}
}

func DefaultBaseFilename(hostname string, now time.Time) string {
	trimmed := strings.TrimSpace(hostname)
	if trimmed == "" {
		trimmed = "computer"
	}

	replacer := strings.NewReplacer(
		"<", "-",
		">", "-",
		":", "-",
		"\"", "-",
		"/", "-",
		"\\", "-",
		"|", "-",
		"?", "-",
		"*", "-",
	)
	trimmed = replacer.Replace(trimmed)
	return fmt.Sprintf("%s-%s.json", trimmed, now.Format("2006-01-02"))
}

func UniquePath(target string) (string, error) {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	ext := filepath.Ext(target)
	name := strings.TrimSuffix(filepath.Base(target), ext)
	if name == "" {
		name = "report"
	}

	candidate := filepath.Join(dir, name+ext)
	for idx := 0; ; idx++ {
		if idx > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, idx, ext))
		}

		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func WriteJSON(path string, v any) error {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}
