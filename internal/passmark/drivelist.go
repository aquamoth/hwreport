package passmark

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The lookup page on harddrivebenchmark.net only resolves exact drive names
// and otherwise returns a generic list, so common models like
// "WD_BLACK SN7100 1TB" were never found. The site's full drive list (about
// 16,000 drives) is downloaded once, kept next to the reports and matched
// locally.

const (
	driveListURL      = "https://www.harddrivebenchmark.net/data"
	driveListPageURL  = "https://www.harddrivebenchmark.net/hdd-mega-page.html"
	driveListMaxAge   = 30 * 24 * time.Hour
	driveListFileName = ".hwoverview-drive-list.json"
)

type DriveListEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DriveMark *int   `json:"drive_mark"`
	Samples   int    `json:"samples"`
	Type      string `json:"type"`
	DetailURL string `json:"detail_url"`
}

type driveListFile struct {
	FetchedAtUTC string           `json:"fetched_at_utc"`
	Entries      []DriveListEntry `json:"entries"`
}

// loadDriveList returns the cached list, downloading it when it is missing or
// older than driveListMaxAge. A stale list is still used if the download fails.
func loadDriveList(ctx context.Context, path string, now time.Time) ([]DriveListEntry, error) {
	var cached driveListFile
	if payload, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(payload, &cached) == nil {
			if fetched, err := time.Parse(time.RFC3339, cached.FetchedAtUTC); err == nil && now.Sub(fetched) < driveListMaxAge && len(cached.Entries) > 0 {
				return cached.Entries, nil
			}
		}
	}

	entries, err := downloadDriveList(ctx)
	if err != nil {
		if len(cached.Entries) > 0 {
			return cached.Entries, nil
		}
		return nil, err
	}

	payload, err := json.Marshal(driveListFile{FetchedAtUTC: now.UTC().Format(time.RFC3339), Entries: entries})
	if err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, payload, 0o644)
	}
	return entries, nil
}

func downloadDriveList(ctx context.Context) ([]DriveListEntry, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 90 * time.Second, Jar: jar}

	// The data feed is empty without the session cookie the list page sets.
	if _, err := getPage(ctx, client, driveListPageURL, nil); err != nil {
		return nil, fmt.Errorf("open drive list page: %w", err)
	}
	body, err := getPage(ctx, client, driveListURL, map[string]string{
		"X-Requested-With": "XMLHttpRequest",
		"Referer":          driveListPageURL,
		"Accept":           "application/json, text/javascript, */*; q=0.01",
	})
	if err != nil {
		return nil, fmt.Errorf("download drive list: %w", err)
	}
	entries, err := parseDriveList(body)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("drive list download was empty")
	}
	return entries, nil
}

func getPage(ctx context.Context, client *http.Client, pageURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", pageURL, resp.Status)
	}
	return ioReadAll(resp)
}

func parseDriveList(payload []byte) ([]DriveListEntry, error) {
	var raw struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("decode drive list: %w", err)
	}

	entries := make([]DriveListEntry, 0, len(raw.Data))
	for _, row := range raw.Data {
		name := strings.TrimSpace(html.UnescapeString(anyString(row["name"])))
		href := html.UnescapeString(anyString(row["href"]))
		if name == "" || href == "" {
			continue
		}
		page := "hdd.php"
		if output, _ := row["output"].(bool); output {
			page = "hdd_lookup.php"
		}
		mark, _ := parseOptionalInt(anyString(row["diskmark"]))
		samples, _ := strconv.Atoi(strings.ReplaceAll(anyString(row["samples"]), ",", ""))
		entries = append(entries, DriveListEntry{
			ID:        anyString(row["id"]),
			Name:      name,
			DriveMark: mark,
			Samples:   samples,
			Type:      anyString(row["type"]),
			DetailURL: "https://www.harddrivebenchmark.net/" + page + "?hdd=" + href,
		})
	}
	return entries, nil
}

func anyString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// Words that say nothing about which drive it is.
var driveNoiseTokens = map[string]bool{
	"NVME": true, "SSD": true, "SATA": true, "M.2": true, "M2": true, "PCIE": true,
	"SOLID": true, "STATE": true, "DRIVE": true, "DISK": true, "HDD": true, "ATA": true, "DEVICE": true,
}

var driveCapacityRegexp = regexp.MustCompile(`^(\d+(?:\.\d+)?)(GB|G|TB|T)$`)

type driveTokens struct {
	words      []string // significant words, capacity and noise removed
	identifier string   // longest word containing a digit: the model code
	capacityGB float64  // 0 when the name has no capacity
}

func tokenizeDriveName(name string) driveTokens {
	var result driveTokens
	seen := map[string]bool{}
	replacer := strings.NewReplacer("_", " ", "-", " ", "/", " ", "(", " ", ")", " ", ",", " ")
	for _, word := range strings.Fields(strings.ToUpper(replacer.Replace(name))) {
		if driveNoiseTokens[word] || seen[word] {
			continue
		}
		if m := driveCapacityRegexp.FindStringSubmatch(word); m != nil {
			value, _ := strconv.ParseFloat(m[1], 64)
			if strings.HasPrefix(m[2], "T") {
				value *= 1000
			}
			if value >= 16 && result.capacityGB == 0 {
				result.capacityGB = value
				continue
			}
		}
		seen[word] = true
		result.words = append(result.words, word)
		if strings.ContainsAny(word, "0123456789") && len(word) > len(result.identifier) {
			result.identifier = word
		}
	}
	return result
}

func sameCapacity(a, b float64) bool {
	if a == 0 || b == 0 {
		return false
	}
	return math.Abs(a-b)/math.Max(a, b) <= 0.1 // 500 GB = 512 GB, 1 TB = 1024 GB
}

// matchDriveList picks the list entry for a drive model as Windows reports it.
// The model code (e.g. SN7100, ST2000DM005, WD30EZRZ) must appear exactly;
// the capacity and the remaining words decide between variants, and words the
// model does not have (PRO, PLUS, HS) count against an entry.
func matchDriveList(model string, entries []DriveListEntry) (DriveListEntry, bool) {
	if entry, ok := matchDriveListPass(model, entries, false); ok {
		return entry, true
	}
	// Second pass ignoring separators: Windows reports "SKC3000S1024G" for the
	// drive PassMark lists as "KINGSTON SKC3000S/1024G".
	return matchDriveListPass(model, entries, true)
}

var nonAlphanumericRegexp = regexp.MustCompile(`[^A-Z0-9]`)

func compactDriveName(value string) string {
	return nonAlphanumericRegexp.ReplaceAllString(strings.ToUpper(value), "")
}

func matchDriveListPass(model string, entries []DriveListEntry, compact bool) (DriveListEntry, bool) {
	query := tokenizeDriveName(model)
	if len(query.words) == 0 {
		return DriveListEntry{}, false
	}
	if compact && len(query.identifier) < 6 {
		return DriveListEntry{}, false
	}
	queryWords := map[string]bool{}
	for _, word := range query.words {
		queryWords[word] = true
	}

	bestIndex := -1
	bestScore := math.MinInt
	for idx, entry := range entries {
		candidate := tokenizeDriveName(entry.Name)
		candidateWords := map[string]bool{}
		for _, word := range candidate.words {
			candidateWords[word] = true
		}

		if compact {
			if !strings.Contains(compactDriveName(entry.Name), compactDriveName(query.identifier)) {
				continue
			}
			candidateWords[query.identifier] = true
		} else if query.identifier != "" {
			if !candidateWords[query.identifier] {
				continue
			}
		} else {
			// No model code: every word must match, e.g. "KIOXIA-EXCERIA BASIC SSD".
			if len(query.words) < 2 {
				continue
			}
			all := true
			for _, word := range query.words {
				if !candidateWords[word] {
					all = false
					break
				}
			}
			if !all {
				continue
			}
		}

		score := 0
		for _, word := range query.words {
			if candidateWords[word] {
				score += 10
			} else {
				score -= 3
			}
		}
		for _, word := range candidate.words {
			if !queryWords[word] {
				score -= 8
			}
		}
		switch {
		case sameCapacity(query.capacityGB, candidate.capacityGB):
			score += 20
		case query.capacityGB > 0 && candidate.capacityGB > 0:
			score -= 15
		case query.capacityGB > 0:
			score -= 5
		}

		if score > bestScore || (score == bestScore && entry.Samples > entries[bestIndex].Samples) {
			bestScore = score
			bestIndex = idx
		}
	}
	if bestIndex < 0 {
		return DriveListEntry{}, false
	}
	return entries[bestIndex], true
}
