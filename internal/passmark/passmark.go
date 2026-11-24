package passmark

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
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

type LookupResult struct {
	Query         string `json:"query"`
	CanonicalName string `json:"canonical_name"`
	CPUMark       *int   `json:"cpu_mark"`
	LookupURL     string `json:"lookup_url"`
	CachedAtUTC   string `json:"cached_at_utc"`
	// FirstSeen is PassMark's "CPU First Seen on Charts", e.g. "Q2 2013":
	// roughly when the CPU came on the market.
	FirstSeen        string `json:"first_seen,omitempty"`
	FirstSeenChecked bool   `json:"first_seen_checked,omitempty"`
	// NotFound records a CPU PassMark doesn't know; see missRetryAfter.
	NotFound bool `json:"not_found,omitempty"`
}

type Cache struct {
	Entries map[string]LookupResult `json:"entries"`
}

type Client struct {
	cachePath string
	cache     Cache
	http      *http.Client
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

func NewClient(cachePath string) (*Client, error) {
	cache := Cache{Entries: map[string]LookupResult{}}
	payload, err := os.ReadFile(cachePath)
	if err == nil {
		if err := json.Unmarshal(payload, &cache); err != nil {
			return nil, fmt.Errorf("decode passmark cache: %w", err)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read passmark cache: %w", err)
	}
	if cache.Entries == nil {
		cache.Entries = map[string]LookupResult{}
	}

	return &Client{
		cachePath: cachePath,
		cache:     cache,
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func (c *Client) Lookup(ctx context.Context, cpuModel string) (LookupResult, error) {
	key := normalizeKey(cpuModel)
	if key == "" {
		return LookupResult{}, fmt.Errorf("empty cpu model")
	}

	if cached, ok := c.cache.Entries[key]; ok && cached.NotFound {
		if recentlyCached(cached.CachedAtUTC, time.Now()) {
			return LookupResult{}, fmt.Errorf("cpu %q is not on PassMark", cpuModel)
		}
		delete(c.cache.Entries, key)
	}
	if cached, ok := c.cache.Entries[key]; ok {
		if !cached.FirstSeenChecked {
			// Entries cached before first_seen existed.
			c.fillFirstSeen(ctx, &cached)
			c.cache.Entries[key] = cached
			if err := c.save(); err != nil {
				return LookupResult{}, err
			}
		}
		return cached, nil
	}

	candidates := lookupCandidates(cpuModel)
	var lastErr error
	siteFailed := false
	for _, candidate := range candidates {
		result, err := c.lookupCandidate(ctx, candidate)
		if err == nil {
			c.fillFirstSeen(ctx, &result)
			c.cache.Entries[key] = result
			if err := c.save(); err != nil {
				return LookupResult{}, err
			}
			return result, nil
		}
		if text := err.Error(); strings.HasPrefix(text, "request passmark lookup") || strings.HasPrefix(text, "passmark lookup returned") || strings.HasPrefix(text, "read passmark lookup") {
			siteFailed = true
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no passmark lookup candidates")
	}
	if !siteFailed {
		// PassMark answered but doesn't know this CPU: don't ask again for a while.
		c.cache.Entries[key] = LookupResult{Query: cpuModel, NotFound: true, CachedAtUTC: time.Now().UTC().Format(time.RFC3339)}
		if err := c.save(); err != nil {
			return LookupResult{}, err
		}
	}
	return LookupResult{}, lastErr
}

func (c *Client) lookupCandidate(ctx context.Context, candidate string) (LookupResult, error) {
	lookupURL := "https://www.cpubenchmark.net/cpu_lookup.php?cpu=" + url.QueryEscape(candidate)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return LookupResult{}, fmt.Errorf("create passmark request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return LookupResult{}, fmt.Errorf("request passmark lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return LookupResult{}, fmt.Errorf("passmark lookup returned %s", resp.Status)
	}

	bodyBytes, err := ioReadAll(resp)
	if err != nil {
		return LookupResult{}, fmt.Errorf("read passmark lookup: %w", err)
	}
	body := string(bodyBytes)

	id, canonicalName, canonicalURL, err := parseCanonical(body)
	if err != nil {
		return LookupResult{}, err
	}

	score, err := parseCPUMark(body, id)
	if err != nil {
		return LookupResult{}, err
	}

	return LookupResult{
		Query:         candidate,
		CanonicalName: canonicalName,
		CPUMark:       &score,
		LookupURL:     canonicalURL,
		CachedAtUTC:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

var (
	lookupIDRegexp  = regexp.MustCompile(`[?&]id=(\d+)`)
	firstSeenRegexp = regexp.MustCompile(`CPU First Seen on Charts:</strong>\s*([^<]+?)\s*</p>`)
)

// fillFirstSeen reads the CPU's own page for its first-seen date. A network
// error leaves the entry unchecked so the next run tries again.
func (c *Client) fillFirstSeen(ctx context.Context, result *LookupResult) {
	idMatch := lookupIDRegexp.FindStringSubmatch(result.LookupURL)
	if idMatch == nil {
		result.FirstSeenChecked = true
		return
	}
	detailURL := "https://www.cpubenchmark.net/cpu.php?cpu=" + url.QueryEscape(result.CanonicalName) + "&id=" + idMatch[1]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, err := ioReadAll(resp)
	if err != nil {
		return
	}
	result.FirstSeen = parseFirstSeen(string(body))
	result.FirstSeenChecked = true
}

func parseFirstSeen(body string) string {
	if m := firstSeenRegexp.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(html.UnescapeString(m[1]))
	}
	return ""
}

func (c *Client) save() error {
	if err := os.MkdirAll(filepath.Dir(c.cachePath), 0o755); err != nil {
		return fmt.Errorf("create passmark cache directory: %w", err)
	}

	payload, err := json.MarshalIndent(c.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode passmark cache: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(c.cachePath, payload, 0o644); err != nil {
		return fmt.Errorf("write passmark cache: %w", err)
	}
	return nil
}

var (
	canonicalRegexp = regexp.MustCompile(`canonical" href="https://www\.cpubenchmark\.net/cpu_lookup\.php\?id=(\d+)&amp;cpu=([^"]+)"`)
)

func parseCanonical(body string) (id string, cpuName string, canonicalURL string, err error) {
	matches := canonicalRegexp.FindStringSubmatch(body)
	if len(matches) != 3 {
		return "", "", "", fmt.Errorf("passmark canonical cpu id not found")
	}

	id = matches[1]
	canonicalCPU, unescapeErr := url.QueryUnescape(html.UnescapeString(matches[2]))
	if unescapeErr != nil {
		return "", "", "", fmt.Errorf("decode passmark canonical cpu name: %w", unescapeErr)
	}

	return id, canonicalCPU, "https://www.cpubenchmark.net/cpu_lookup.php?id=" + id + "&cpu=" + url.QueryEscape(canonicalCPU), nil
}

func parseCPUMark(body, id string) (int, error) {
	pattern := regexp.MustCompile(`id="rk` + regexp.QuoteMeta(id) + `".*?<span class="count">([\d,]+)</span>`)
	matches := pattern.FindStringSubmatch(body)
	if len(matches) != 2 {
		return 0, fmt.Errorf("passmark cpu mark not found for cpu id %s", id)
	}

	value, err := strconv.Atoi(strings.ReplaceAll(matches[1], ",", ""))
	if err != nil {
		return 0, fmt.Errorf("parse passmark cpu mark: %w", err)
	}
	return value, nil
}

func lookupCandidates(cpuModel string) []string {
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

	add(cpuModel)
	add(regexp.MustCompile(`\s+w/\s+.*$`).ReplaceAllString(cpuModel, ""))
	add(regexp.MustCompile(`\s+with\s+.*$`).ReplaceAllString(cpuModel, ""))
	add(regexp.MustCompile(`\s+@.*$`).ReplaceAllString(cpuModel, ""))
	add(strings.ReplaceAll(cpuModel, " Processor", ""))

	if len(candidates) > 1 {
		// Keep the original string first, but sort fallback variants for stable behavior.
		rest := append([]string(nil), candidates[1:]...)
		sort.Strings(rest)
		candidates = append(candidates[:1], rest...)
	}
	return candidates
}

func normalizeKey(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func ioReadAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}
