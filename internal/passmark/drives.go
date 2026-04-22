package passmark

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DriveLookupResult struct {
	Query               string   `json:"query"`
	CanonicalName       string   `json:"canonical_name"`
	DriveMark           *int     `json:"drive_mark"`
	SequentialReadMBps  *float64 `json:"sequential_read_mbps"`
	SequentialWriteMBps *float64 `json:"sequential_write_mbps"`
	RandomReadWriteMBps *float64 `json:"random_read_write_mbps"`
	IOPS4KQD1MBps       *float64 `json:"iops_4kqd1_mbps"`
	LookupURL           string   `json:"lookup_url"`
	CachedAtUTC         string   `json:"cached_at_utc"`
}

type DriveCache struct {
	Entries map[string]DriveLookupResult `json:"entries"`
}

type DriveClient struct {
	cachePath string
	cache     DriveCache
	http      *http.Client
}

func NewDriveClient(cachePath string) (*DriveClient, error) {
	cache := DriveCache{Entries: map[string]DriveLookupResult{}}
	payload, err := os.ReadFile(cachePath)
	if err == nil {
		if err := json.Unmarshal(payload, &cache); err != nil {
			return nil, fmt.Errorf("decode drive benchmark cache: %w", err)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read drive benchmark cache: %w", err)
	}
	if cache.Entries == nil {
		cache.Entries = map[string]DriveLookupResult{}
	}

	return &DriveClient{
		cachePath: cachePath,
		cache:     cache,
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func (c *DriveClient) Lookup(ctx context.Context, driveModel string) (DriveLookupResult, error) {
	key := normalizeKey(driveModel)
	if key == "" {
		return DriveLookupResult{}, fmt.Errorf("empty drive model")
	}

	if cached, ok := c.cache.Entries[key]; ok {
		return cached, nil
	}

	candidates := driveLookupCandidates(driveModel)
	var lastErr error
	for _, candidate := range candidates {
		result, err := c.lookupCandidate(ctx, candidate)
		if err == nil {
			c.cache.Entries[key] = result
			if err := c.save(); err != nil {
				return DriveLookupResult{}, err
			}
			return result, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no drive benchmark lookup candidates")
	}
	return DriveLookupResult{}, lastErr
}

func (c *DriveClient) lookupCandidate(ctx context.Context, candidate string) (DriveLookupResult, error) {
	lookupURL := "https://www.harddrivebenchmark.net/hdd_lookup.php?hdd=" + url.QueryEscape(candidate)
	body, err := c.fetchPage(ctx, lookupURL)
	if err != nil {
		return DriveLookupResult{}, err
	}

	id, canonicalName, _, err := parseDriveLookupCanonical(body)
	if err != nil {
		return DriveLookupResult{}, err
	}

	detailURL, entryName, listDriveMark, err := parseDriveLookupEntry(body, id)
	if err != nil {
		return DriveLookupResult{}, err
	}
	if strings.TrimSpace(entryName) != "" {
		canonicalName = entryName
	}

	detailBody, err := c.fetchPage(ctx, detailURL)
	if err != nil {
		return DriveLookupResult{}, err
	}

	canonicalDetailName, canonicalDetailURL, err := parseDriveDetailCanonical(detailBody)
	if err != nil {
		return DriveLookupResult{}, err
	}

	details, err := parseDriveDetailMetrics(detailBody)
	if err != nil {
		return DriveLookupResult{}, err
	}
	if details.DriveMark == nil {
		details.DriveMark = listDriveMark
	}

	if strings.TrimSpace(canonicalDetailName) != "" {
		canonicalName = canonicalDetailName
	}

	return DriveLookupResult{
		Query:               candidate,
		CanonicalName:       canonicalName,
		DriveMark:           details.DriveMark,
		SequentialReadMBps:  details.SequentialReadMBps,
		SequentialWriteMBps: details.SequentialWriteMBps,
		RandomReadWriteMBps: details.RandomReadWriteMBps,
		IOPS4KQD1MBps:       details.IOPS4KQD1MBps,
		LookupURL:           canonicalDetailURL,
		CachedAtUTC:         time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (c *DriveClient) fetchPage(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create drive benchmark request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request drive benchmark lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("drive benchmark lookup returned %s", resp.Status)
	}

	bodyBytes, err := ioReadAll(resp)
	if err != nil {
		return "", fmt.Errorf("read drive benchmark lookup: %w", err)
	}
	return string(bodyBytes), nil
}

func (c *DriveClient) save() error {
	if err := os.MkdirAll(filepath.Dir(c.cachePath), 0o755); err != nil {
		return fmt.Errorf("create drive benchmark cache directory: %w", err)
	}

	payload, err := json.MarshalIndent(c.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode drive benchmark cache: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(c.cachePath, payload, 0o644); err != nil {
		return fmt.Errorf("write drive benchmark cache: %w", err)
	}
	return nil
}

type driveDetailMetrics struct {
	DriveMark           *int
	SequentialReadMBps  *float64
	SequentialWriteMBps *float64
	RandomReadWriteMBps *float64
	IOPS4KQD1MBps       *float64
}

var (
	driveLookupCanonicalRegexp = regexp.MustCompile(`canonical" href="https://www\.harddrivebenchmark\.net/hdd_lookup\.php\?id=(\d+)&amp;hdd=([^"]+)"`)
)

func parseDriveLookupCanonical(body string) (id string, driveName string, canonicalURL string, err error) {
	matches := driveLookupCanonicalRegexp.FindStringSubmatch(body)
	if len(matches) != 3 {
		return "", "", "", fmt.Errorf("drive benchmark canonical drive id not found")
	}

	id = matches[1]
	canonicalDrive, unescapeErr := url.QueryUnescape(html.UnescapeString(matches[2]))
	if unescapeErr != nil {
		return "", "", "", fmt.Errorf("decode drive benchmark canonical drive name: %w", unescapeErr)
	}

	return id, canonicalDrive, "https://www.harddrivebenchmark.net/hdd_lookup.php?id=" + id + "&hdd=" + url.QueryEscape(canonicalDrive), nil
}

func parseDriveLookupEntry(body, id string) (detailURL string, driveName string, driveMark *int, err error) {
	pattern := regexp.MustCompile(`(?s)<li id="pk` + regexp.QuoteMeta(id) + `">.*?<a href="([^"]*?/hdd\.php\?hdd=[^"]+?&amp;id=` + regexp.QuoteMeta(id) + `)".*?<span class="prdname"\s*>([^<]+)</span>.*?<span class="mark-neww"\s*>([^<]+)</span>`)
	matches := pattern.FindStringSubmatch(body)
	if len(matches) != 4 {
		return "", "", nil, fmt.Errorf("drive benchmark lookup result not found for drive id %s", id)
	}

	parsedMark, err := parseOptionalInt(matches[3])
	if err != nil {
		return "", "", nil, fmt.Errorf("parse drive benchmark drive mark: %w", err)
	}

	relURL := html.UnescapeString(matches[1])
	if strings.HasPrefix(relURL, "http://") || strings.HasPrefix(relURL, "https://") {
		detailURL = relURL
	} else {
		detailURL = "https://www.harddrivebenchmark.net" + relURL
	}

	return detailURL, html.UnescapeString(strings.TrimSpace(matches[2])), parsedMark, nil
}

func parseDriveDetailCanonical(body string) (driveName string, canonicalURL string, err error) {
	pattern := regexp.MustCompile(`canonical" href="(https://www\.harddrivebenchmark\.net/hdd\.php\?hdd=([^"&]+)&amp;id=\d+)"`)
	matches := pattern.FindStringSubmatch(body)
	if len(matches) != 3 {
		return "", "", fmt.Errorf("drive benchmark detail canonical url not found")
	}

	canonicalName, unescapeErr := url.QueryUnescape(html.UnescapeString(matches[2]))
	if unescapeErr != nil {
		return "", "", fmt.Errorf("decode drive benchmark detail canonical drive name: %w", unescapeErr)
	}

	return canonicalName, html.UnescapeString(matches[1]), nil
}

func parseDriveDetailMetrics(body string) (driveDetailMetrics, error) {
	var details driveDetailMetrics
	var err error

	driveMarkPattern := regexp.MustCompile(`(?s)Average Drive Rating.*?<span[^>]*>\s*([\d,]+)\s*</span>`)
	if matches := driveMarkPattern.FindStringSubmatch(body); len(matches) == 2 {
		details.DriveMark, err = parseOptionalInt(matches[1])
		if err != nil {
			return driveDetailMetrics{}, fmt.Errorf("parse drive detail drive mark: %w", err)
		}
	}

	if details.SequentialReadMBps, err = parseMetricFloat(body, "Sequential Read"); err != nil {
		return driveDetailMetrics{}, err
	}
	if details.SequentialWriteMBps, err = parseMetricFloat(body, "Sequential Write"); err != nil {
		return driveDetailMetrics{}, err
	}
	if details.RandomReadWriteMBps, err = parseMetricFloat(body, "Random Seek Read Write (IOPS 32KQD20)"); err != nil {
		return driveDetailMetrics{}, err
	}
	if details.IOPS4KQD1MBps, err = parseMetricFloat(body, "IOPS 4KQD1"); err != nil {
		return driveDetailMetrics{}, err
	}

	return details, nil
}

func parseMetricFloat(body, label string) (*float64, error) {
	pattern := regexp.MustCompile(`(?s)<tr[^>]*>\s*<th>` + regexp.QuoteMeta(label) + `</th>\s*<td>\s*([\d.,]+)\s*MBytes/Sec\s*</td>`)
	matches := pattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		return nil, fmt.Errorf("drive benchmark metric not found for %q", label)
	}

	value, err := strconv.ParseFloat(strings.ReplaceAll(matches[1], ",", ""), 64)
	if err != nil {
		return nil, fmt.Errorf("parse drive benchmark metric %q: %w", label, err)
	}
	return &value, nil
}

func parseOptionalInt(value string) (*int, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" || strings.EqualFold(value, "NA") {
		return nil, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func driveLookupCandidates(driveModel string) []string {
	var candidates []string
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			return
		}
		key := normalizeKey(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, value)
	}

	add(driveModel)
	add(regexp.MustCompile(`\s+\[[^\]]+\]`).ReplaceAllString(driveModel, ""))
	add(regexp.MustCompile(`\s+\(.*?\)`).ReplaceAllString(driveModel, ""))
	add(strings.ReplaceAll(driveModel, "NVMe", ""))
	add(strings.ReplaceAll(driveModel, "SSD", ""))

	if len(candidates) > 1 {
		rest := append([]string(nil), candidates[1:]...)
		sort.Strings(rest)
		candidates = append(candidates[:1], rest...)
	}
	return candidates
}
