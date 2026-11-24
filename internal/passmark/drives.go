package passmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
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
	// NotFound records a drive that isn't in PassMark's list, so it isn't
	// looked up again until missRetryAfter has passed.
	NotFound bool `json:"not_found,omitempty"`
}

// missRetryAfter is how long a drive or CPU that PassMark doesn't know is left
// alone before it is looked up again.
const missRetryAfter = 30 * 24 * time.Hour

func recentlyCached(cachedAtUTC string, now time.Time) bool {
	cachedAt, err := time.Parse(time.RFC3339, cachedAtUTC)
	return err == nil && now.Sub(cachedAt) < missRetryAfter
}

type DriveCache struct {
	Entries map[string]DriveLookupResult `json:"entries"`
}

type DriveClient struct {
	cachePath  string
	cache      DriveCache
	http       *http.Client
	listPath   string
	list       []DriveListEntry
	listLoaded bool
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
		listPath: filepath.Join(filepath.Dir(cachePath), driveListFileName),
	}, nil
}

func (c *DriveClient) driveList(ctx context.Context) []DriveListEntry {
	if !c.listLoaded {
		c.listLoaded = true
		if list, err := loadDriveList(ctx, c.listPath, time.Now()); err == nil {
			c.list = list
		}
	}
	return c.list
}

// lookupViaDriveList matches the model against PassMark's full drive list and
// reads the speed figures from the drive's own page. If that page can't be read,
// the Drive Mark from the list is still returned.
func (c *DriveClient) lookupViaDriveList(ctx context.Context, driveModel string) (DriveLookupResult, bool) {
	entry, ok := matchDriveList(driveModel, c.driveList(ctx))
	if !ok {
		return DriveLookupResult{}, false
	}

	result := DriveLookupResult{
		Query:         driveModel,
		CanonicalName: entry.Name,
		DriveMark:     entry.DriveMark,
		LookupURL:     entry.DetailURL,
		CachedAtUTC:   time.Now().UTC().Format(time.RFC3339),
	}
	detailBody, detailURL, _, _, err := c.fetchDriveDetailBody(ctx, driveModel, driveLookupEntry{ID: entry.ID, DetailURL: entry.DetailURL, DriveName: entry.Name, DriveMark: entry.DriveMark})
	if err != nil {
		return result, true
	}
	if details, err := parseDriveDetailMetrics(detailBody); err == nil {
		if details.DriveMark != nil {
			result.DriveMark = details.DriveMark
		}
		result.SequentialReadMBps = details.SequentialReadMBps
		result.SequentialWriteMBps = details.SequentialWriteMBps
		result.RandomReadWriteMBps = details.RandomReadWriteMBps
		result.IOPS4KQD1MBps = details.IOPS4KQD1MBps
	}
	if detailURL != "" {
		result.LookupURL = detailURL
	}
	return result, true
}

func (c *DriveClient) Lookup(ctx context.Context, driveModel string) (DriveLookupResult, error) {
	key := normalizeKey(driveModel)
	if key == "" {
		return DriveLookupResult{}, fmt.Errorf("empty drive model")
	}

	now := time.Now()
	if cached, ok := c.cache.Entries[key]; ok {
		if !cached.NotFound {
			return cached, nil
		}
		if recentlyCached(cached.CachedAtUTC, now) {
			return DriveLookupResult{}, fmt.Errorf("drive %q is not in PassMark's drive list", driveModel)
		}
	}

	if result, ok := c.lookupViaDriveList(ctx, driveModel); ok {
		c.cache.Entries[key] = result
		if err := c.save(); err != nil {
			return DriveLookupResult{}, err
		}
		return result, nil
	}
	if len(c.driveList(ctx)) > 0 {
		// The full list doesn't have it; the exact-name lookup won't either.
		c.cache.Entries[key] = DriveLookupResult{Query: driveModel, NotFound: true, CachedAtUTC: now.UTC().Format(time.RFC3339)}
		if err := c.save(); err != nil {
			return DriveLookupResult{}, err
		}
		return DriveLookupResult{}, fmt.Errorf("drive %q is not in PassMark's drive list", driveModel)
	}

	// Fallback when the drive list can't be downloaded: the exact-name lookup.
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
	result, err := c.lookupCandidateViaLookupPage(ctx, candidate)
	if err == nil {
		return result, nil
	}

	searchResult, searchErr := c.lookupCandidateViaSiteSearch(ctx, candidate)
	if searchErr == nil {
		return searchResult, nil
	}
	return DriveLookupResult{}, err
}

func (c *DriveClient) lookupCandidateViaLookupPage(ctx context.Context, candidate string) (DriveLookupResult, error) {
	lookupURL := "https://www.harddrivebenchmark.net/hdd_lookup.php?hdd=" + url.QueryEscape(candidate)
	body, err := c.fetchPage(ctx, lookupURL)
	if err != nil {
		return DriveLookupResult{}, err
	}

	id := ""
	canonicalName := ""
	if parsedID, parsedName, _, parseErr := parseDriveLookupCanonical(body); parseErr == nil {
		id = parsedID
		canonicalName = parsedName
	}

	entry, err := chooseDriveLookupEntry(candidate, body, id)
	if err != nil {
		return DriveLookupResult{}, err
	}
	if strings.TrimSpace(entry.DriveName) != "" {
		canonicalName = entry.DriveName
	}

	detailBody, canonicalDetailURL, canonicalDetailName, listDriveMark, err := c.fetchDriveDetailBody(ctx, candidate, entry)
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
	if !hasStrongDriveIdentifierMatch(candidate, canonicalName) {
		return DriveLookupResult{}, fmt.Errorf("drive benchmark weak match for %q -> %q", candidate, canonicalName)
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

func (c *DriveClient) lookupCandidateViaSiteSearch(ctx context.Context, candidate string) (DriveLookupResult, error) {
	searchURL := "https://www.passmark.com/search/zoomsearch.php?zoom_query=" + url.QueryEscape(candidate)
	body, err := c.fetchPage(ctx, searchURL)
	if err != nil {
		return DriveLookupResult{}, err
	}

	entry, err := chooseDriveSearchEntry(candidate, body)
	if err != nil {
		return DriveLookupResult{}, err
	}

	detailBody, canonicalDetailURL, canonicalDetailName, listDriveMark, err := c.fetchDriveDetailBody(ctx, candidate, driveLookupEntry{
		DetailURL: entry.DetailURL,
		DriveName: entry.DriveName,
	})
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
	if !hasStrongDriveIdentifierMatch(candidate, canonicalDetailName) {
		return DriveLookupResult{}, fmt.Errorf("drive benchmark weak site-search match for %q -> %q", candidate, canonicalDetailName)
	}

	return DriveLookupResult{
		Query:               candidate,
		CanonicalName:       canonicalDetailName,
		DriveMark:           details.DriveMark,
		SequentialReadMBps:  details.SequentialReadMBps,
		SequentialWriteMBps: details.SequentialWriteMBps,
		RandomReadWriteMBps: details.RandomReadWriteMBps,
		IOPS4KQD1MBps:       details.IOPS4KQD1MBps,
		LookupURL:           canonicalDetailURL,
		CachedAtUTC:         time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (c *DriveClient) fetchDriveDetailBody(ctx context.Context, query string, entry driveLookupEntry) (body string, canonicalDetailURL string, canonicalDetailName string, listDriveMark *int, err error) {
	body, err = c.fetchPage(ctx, entry.DetailURL)
	if err != nil {
		return "", "", "", nil, err
	}

	canonicalDetailName, canonicalDetailURL, err = parseDriveDetailCanonical(body)
	if err == nil {
		return body, canonicalDetailURL, canonicalDetailName, entry.DriveMark, nil
	}

	canonicalID := ""
	if parsedID, _, _, parseErr := parseDriveLookupCanonical(body); parseErr == nil {
		canonicalID = parsedID
	}

	nestedEntry, nestedErr := chooseDriveLookupEntry(query, body, canonicalID)
	if nestedErr != nil {
		return "", "", "", nil, err
	}
	if nestedEntry.DetailURL == entry.DetailURL {
		return "", "", "", nil, err
	}

	body, err = c.fetchPage(ctx, nestedEntry.DetailURL)
	if err != nil {
		return "", "", "", nil, err
	}

	canonicalDetailName, canonicalDetailURL, err = parseDriveDetailCanonical(body)
	if err != nil {
		return "", "", "", nil, err
	}

	if nestedEntry.DriveMark != nil {
		return body, canonicalDetailURL, canonicalDetailName, nestedEntry.DriveMark, nil
	}
	return body, canonicalDetailURL, canonicalDetailName, entry.DriveMark, nil
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

func IsNetworkPermissionError(err error) bool {
	if err == nil {
		return false
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		text := strings.ToLower(opErr.Err.Error())
		if strings.Contains(text, "forbidden by its access permissions") {
			return true
		}
	}

	text := strings.ToLower(err.Error())
	return strings.Contains(text, "forbidden by its access permissions")
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

type driveLookupEntry struct {
	ID        string
	DetailURL string
	DriveName string
	DriveMark *int
}

type driveSearchEntry struct {
	DetailURL string
	DriveName string
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

func chooseDriveLookupEntry(query, body, canonicalID string) (driveLookupEntry, error) {
	entries, err := parseDriveLookupEntries(body)
	if err != nil {
		return driveLookupEntry{}, err
	}

	bestIndex := -1
	bestScore := -1
	for idx, entry := range entries {
		score := scoreDriveLookupEntry(query, entry, canonicalID)
		if score > bestScore {
			bestScore = score
			bestIndex = idx
		}
	}
	if bestIndex < 0 {
		return driveLookupEntry{}, fmt.Errorf("drive benchmark lookup result not found")
	}
	return entries[bestIndex], nil
}

func parseDriveLookupEntries(body string) ([]driveLookupEntry, error) {
	var entries []driveLookupEntry
	entries = append(entries, parseDriveLookupCardEntries(body)...)
	entries = append(entries, parseDriveLookupTableEntries(body)...)
	if len(entries) == 0 {
		return nil, fmt.Errorf("drive benchmark lookup results not found")
	}

	deduped := make([]driveLookupEntry, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		key := entry.ID + "|" + normalizeKey(entry.DriveName)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, entry)
	}
	return deduped, nil
}

func chooseDriveSearchEntry(query string, body string) (driveSearchEntry, error) {
	entries, err := parseDriveSearchEntries(body)
	if err != nil {
		return driveSearchEntry{}, err
	}

	bestIndex := -1
	bestScore := -1
	for idx, entry := range entries {
		score := driveIdentifierMatchScore(query, entry.DriveName) + scoreDriveSearchURL(query, entry.DetailURL)
		if score > bestScore {
			bestScore = score
			bestIndex = idx
		}
	}
	if bestIndex < 0 {
		return driveSearchEntry{}, fmt.Errorf("drive benchmark site search result not found")
	}
	return entries[bestIndex], nil
}

func parseDriveSearchEntries(body string) ([]driveSearchEntry, error) {
	pattern := regexp.MustCompile(`(?s)<div class="result_title">.*?<a href="(https://www\.harddrivebenchmark\.net/(?:hdd|hdd_lookup)\.php[^"]+)"[^>]*>(.*?)</a>`)
	matches := pattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("drive benchmark site search results not found")
	}

	var entries []driveSearchEntry
	seen := map[string]struct{}{}
	for _, match := range matches {
		detailURL := html.UnescapeString(match[1])
		driveName := stripTags(html.UnescapeString(match[2]))
		driveName = strings.ReplaceAll(driveName, " - Benchmark performance", "")
		driveName = strings.TrimSpace(strings.ReplaceAll(driveName, " - Benchmark results", ""))
		if detailURL == "" || driveName == "" {
			continue
		}
		if _, ok := seen[detailURL]; ok {
			continue
		}
		seen[detailURL] = struct{}{}
		entries = append(entries, driveSearchEntry{
			DetailURL: detailURL,
			DriveName: driveName,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("drive benchmark site search results not found")
	}
	return entries, nil
}

func parseDriveLookupCardEntries(body string) []driveLookupEntry {
	pattern := regexp.MustCompile(`(?s)<li id="pk(\d+)">.*?<a href="([^"]*?/hdd\.php\?hdd=[^"]+?&amp;id=\d+)".*?<span class="prdname"\s*>([^<]+)</span>.*?<span class="mark-neww"\s*>([^<]+)</span>`)
	matches := pattern.FindAllStringSubmatch(body, -1)
	return buildDriveLookupEntries(matches, 1, 2, 3, 4)
}

func parseDriveLookupTableEntries(body string) []driveLookupEntry {
	pattern := regexp.MustCompile(`(?s)<tr><td><a href="([^"]*?/hdd_lookup\.php\?hdd=[^"]+?&amp;id=(\d+))">([^<]+)</a></td><td>[^<]*</td><td>([\d,]+)</td>`)
	matches := pattern.FindAllStringSubmatch(body, -1)
	return buildDriveLookupEntries(matches, 2, 1, 3, 4)
}

func buildDriveLookupEntries(matches [][]string, idIndex, urlIndex, nameIndex, markIndex int) []driveLookupEntry {
	entries := make([]driveLookupEntry, 0, len(matches))
	for _, match := range matches {
		parsedMark, err := parseOptionalInt(match[markIndex])
		if err != nil {
			continue
		}

		relURL := html.UnescapeString(match[urlIndex])
		detailURL := relURL
		if !strings.HasPrefix(relURL, "http://") && !strings.HasPrefix(relURL, "https://") {
			detailURL = "https://www.harddrivebenchmark.net" + relURL
		}

		entries = append(entries, driveLookupEntry{
			ID:        match[idIndex],
			DetailURL: detailURL,
			DriveName: html.UnescapeString(strings.TrimSpace(match[nameIndex])),
			DriveMark: parsedMark,
		})
	}
	return entries
}

func scoreDriveLookupEntry(query string, entry driveLookupEntry, canonicalID string) int {
	queryNorm := normalizeDriveComparison(query)
	nameNorm := normalizeDriveComparison(entry.DriveName)

	score := 0
	if entry.ID == canonicalID {
		score += 20
	}
	if queryNorm == nameNorm {
		score += 2000
	}
	if queryNorm != "" && strings.Contains(nameNorm, queryNorm) {
		score += 1200 + len(queryNorm)
	}
	if nameNorm != "" && strings.Contains(queryNorm, nameNorm) {
		score += 900 + len(nameNorm)
	}
	score += driveIdentifierMatchScore(query, entry.DriveName)
	score += commonPrefixLen(queryNorm, nameNorm) * 10
	score += tokenOverlapScore(queryNorm, nameNorm)
	if entry.DriveMark != nil {
		score += 1
	}
	return score
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

	// Pages for older drives often have a Drive Mark but no speed table; the
	// figures that are missing stay nil.
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
	if details.DriveMark == nil && details.SequentialReadMBps == nil && details.SequentialWriteMBps == nil &&
		details.RandomReadWriteMBps == nil && details.IOPS4KQD1MBps == nil {
		return driveDetailMetrics{}, fmt.Errorf("drive benchmark page has no drive mark or speed figures")
	}

	return details, nil
}

// parseMetricFloat returns nil when the page has no row for the label.
func parseMetricFloat(body, label string) (*float64, error) {
	pattern := regexp.MustCompile(`(?s)<tr[^>]*>\s*<th>` + regexp.QuoteMeta(label) + `</th>\s*<td>\s*([\d.,]+)\s*MBytes/Sec\s*</td>`)
	matches := pattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		return nil, nil
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

	fields := strings.Fields(driveModel)
	if len(fields) > 0 {
		if looksLikeDriveIdentifier(fields[len(fields)-1]) {
			add(fields[len(fields)-1])
		}
	}
	for _, token := range fields {
		base, capacity, ok := splitDriveCapacitySuffix(token)
		if !ok {
			continue
		}
		add(strings.Replace(driveModel, token, base+"/"+capacity, 1))
		add(strings.Replace(driveModel, token, base+" "+capacity, 1))
		add(base + "/" + capacity)
		add(base)
	}
	for _, value := range progressiveDrivePrefixes(driveModel) {
		add(value)
	}

	if len(candidates) > 1 {
		rest := append([]string(nil), candidates[1:]...)
		sort.Strings(rest)
		candidates = append(candidates[:1], rest...)
	}
	return candidates
}

func splitDriveCapacitySuffix(value string) (base string, capacity string, ok bool) {
	value = strings.TrimSpace(value)
	pattern := regexp.MustCompile(`^(.+?)(\d{3,5}(?:GB|G|TB|T))$`)
	matches := pattern.FindStringSubmatch(strings.ToUpper(value))
	if len(matches) != 3 {
		return "", "", false
	}
	if len(matches[1]) < 4 {
		return "", "", false
	}
	return matches[1], matches[2], true
}

func progressiveDrivePrefixes(value string) []string {
	var prefixes []string
	seen := map[string]struct{}{}
	add := func(candidate string) {
		candidate = strings.Join(strings.Fields(strings.TrimSpace(candidate)), " ")
		if len(candidate) < 4 {
			return
		}
		key := normalizeKey(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		prefixes = append(prefixes, candidate)
	}

	for _, token := range strings.Fields(value) {
		if looksLikeDriveIdentifier(token) {
			add(token)
		}

		current := token
		for _, sep := range []string{"-", "/", "_"} {
			if idx := strings.Index(current, sep); idx > 0 {
				current = current[:idx]
				if looksLikeDriveIdentifier(current) {
					add(current)
				}
			}
		}
	}

	return prefixes
}

func looksLikeDriveIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 4 {
		return false
	}

	upper := strings.ToUpper(value)
	hasDigit := regexp.MustCompile(`\d`).MatchString(upper)
	hasSeparator := strings.ContainsAny(upper, "-/_")
	if hasDigit || hasSeparator {
		return true
	}

	// Avoid broad vendor/common-name tokens like "Micron", "SAMSUNG", "Green", "SSSTC".
	return false
}

func normalizeDriveComparison(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^A-Z0-9]`).ReplaceAllString(value, "")
	return value
}

func commonPrefixLen(left, right string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	count := 0
	for idx := 0; idx < limit; idx++ {
		if left[idx] != right[idx] {
			break
		}
		count++
	}
	return count
}

func tokenOverlapScore(queryNorm, nameNorm string) int {
	for width := len(queryNorm); width >= 6; width-- {
		for start := 0; start+width <= len(queryNorm); start++ {
			if strings.Contains(nameNorm, queryNorm[start:start+width]) {
				return width
			}
		}
	}
	return 0
}

func driveIdentifierMatchScore(query string, candidate string) int {
	candidateNorm := normalizeDriveComparison(candidate)
	score := 0
	for _, piece := range driveIdentifierPieces(query) {
		pieceNorm := normalizeDriveComparison(piece)
		if pieceNorm == "" {
			continue
		}
		if strings.Contains(candidateNorm, pieceNorm) {
			score += 3000 + len(pieceNorm)*20
			continue
		}
		for _, candidatePiece := range driveIdentifierPieces(candidate) {
			candidatePieceNorm := normalizeDriveComparison(candidatePiece)
			if candidatePieceNorm == "" {
				continue
			}
			if pieceNorm == candidatePieceNorm {
				score += 2500 + len(pieceNorm)*20
				continue
			}
			if shared := commonPrefixLen(pieceNorm, candidatePieceNorm); shared >= 6 {
				score += shared * 5
			}
		}
	}
	return score
}

func hasStrongDriveIdentifierMatch(query string, candidate string) bool {
	queryNorm := normalizeDriveComparison(query)
	candidateNorm := normalizeDriveComparison(candidate)
	if queryNorm != "" && candidateNorm == queryNorm {
		return true
	}
	for _, piece := range driveIdentifierPieces(query) {
		pieceNorm := normalizeDriveComparison(piece)
		if len(pieceNorm) < 8 {
			continue
		}
		if strings.Contains(candidateNorm, pieceNorm) {
			return true
		}
	}
	return false
}

func scoreDriveSearchURL(query string, detailURL string) int {
	return driveIdentifierMatchScore(query, detailURL)
}

func driveIdentifierPieces(value string) []string {
	var pieces []string
	seen := map[string]struct{}{}
	add := func(piece string) {
		piece = strings.ToUpper(strings.TrimSpace(piece))
		piece = strings.Trim(piece, "-/_()[]")
		if len(piece) < 4 || !looksLikeDriveIdentifier(piece) {
			return
		}
		key := normalizeDriveComparison(piece)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		pieces = append(pieces, piece)
	}

	for _, token := range strings.Fields(value) {
		add(token)

		current := strings.ToUpper(strings.TrimSpace(token))
		for _, sep := range []string{"-", "/", "_"} {
			if idx := strings.Index(current, sep); idx > 0 {
				current = current[:idx]
				add(current)
			}
		}

		if base, _, ok := splitDriveCapacitySuffix(token); ok {
			add(base)
		}
	}

	sort.SliceStable(pieces, func(i, j int) bool {
		return len(normalizeDriveComparison(pieces[i])) > len(normalizeDriveComparison(pieces[j]))
	})
	return pieces
}

func stripTags(value string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, "")
}
