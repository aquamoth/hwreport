// Package once records that a named collection run has completed on this
// computer, so a scheduled task that fires repeatedly reports only once.
package once

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidateKey reports whether key is usable as part of a marker file name.
func ValidateKey(key string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("invalid --once key %q: use 1-64 letters, digits, '.', '_' or '-'", key)
	}
	return nil
}

// DefaultDir is the machine-wide marker folder, shared by all users of the computer.
func DefaultDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "hwreport")
}

// MarkerPath returns the marker file for key in dir.
func MarkerPath(dir, key string) string {
	return filepath.Join(dir, "once-"+key+".done")
}

// Done reports whether a run with key has already completed.
func Done(dir, key string) (bool, error) {
	_, err := os.Stat(MarkerPath(dir, key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Mark records that a run with key completed, writing the report path for reference.
func Mark(dir, key, reportPath string, now time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("%s\n%s\n", now.UTC().Format(time.RFC3339), reportPath)
	return os.WriteFile(MarkerPath(dir, key), []byte(content), 0o644)
}
