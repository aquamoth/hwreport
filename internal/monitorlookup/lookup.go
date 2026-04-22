package monitorlookup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

type LookupResult struct {
	Query          string   `json:"query"`
	CanonicalName  string   `json:"canonical_name"`
	PhysicalWidth  *float64 `json:"physical_width"`
	PhysicalHeight *float64 `json:"physical_height"`
	PhysicalUnit   string   `json:"physical_unit"`
	LookupURL      string   `json:"lookup_url"`
	CachedAtUTC    string   `json:"cached_at_utc"`
}

type Cache struct {
	Entries map[string]LookupResult `json:"entries"`
}

type Client struct {
	cachePath string
	cache     Cache
	http      *http.Client
}

func NewClient(cachePath string) (*Client, error) {
	cache := Cache{Entries: map[string]LookupResult{}}
	payload, err := os.ReadFile(cachePath)
	if err == nil {
		if err := json.Unmarshal(payload, &cache); err != nil {
			return nil, fmt.Errorf("decode monitor lookup cache: %w", err)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read monitor lookup cache: %w", err)
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

func (c *Client) Lookup(ctx context.Context, pnpID string) (LookupResult, error) {
	pnpID = normalizePNPID(pnpID)
	if pnpID == "" {
		return LookupResult{}, fmt.Errorf("empty monitor pnp id")
	}
	if cached, ok := c.cache.Entries[pnpID]; ok {
		return cached, nil
	}

	vendor := pnpID[:3]
	lookupURL := "https://linux-hardware.org/?id=eisa:" + strings.ToLower(vendor) + "-" + strings.ToLower(pnpID)
	body, err := c.fetchPage(ctx, lookupURL)
	if err != nil {
		return LookupResult{}, err
	}

	name, width, height, err := parseLinuxHardwareMonitor(body)
	if err != nil {
		return LookupResult{}, err
	}

	result := LookupResult{
		Query:          pnpID,
		CanonicalName:  name,
		PhysicalWidth:  width,
		PhysicalHeight: height,
		PhysicalUnit:   "cm",
		LookupURL:      lookupURL,
		CachedAtUTC:    time.Now().UTC().Format(time.RFC3339),
	}
	c.cache.Entries[pnpID] = result
	if err := c.save(); err != nil {
		return LookupResult{}, err
	}
	return result, nil
}

func (c *Client) fetchPage(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("create monitor lookup request: %w", err)
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("request monitor lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("monitor lookup returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read monitor lookup: %w", err)
	}
	return string(body), nil
}

func (c *Client) save() error {
	if err := os.MkdirAll(filepath.Dir(c.cachePath), 0o755); err != nil {
		return fmt.Errorf("create monitor lookup cache directory: %w", err)
	}

	payload, err := json.MarshalIndent(c.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode monitor lookup cache: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(c.cachePath, payload, 0o644); err != nil {
		return fmt.Errorf("write monitor lookup cache: %w", err)
	}
	return nil
}

func normalizePNPID(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if regexp.MustCompile(`^[A-Z]{3}[0-9A-F]{4}$`).MatchString(value) {
		return value
	}
	return ""
}

func parseLinuxHardwareMonitor(body string) (string, *float64, *float64, error) {
	titlePattern := regexp.MustCompile(`Device '([^']+)'`)
	titleMatches := titlePattern.FindStringSubmatch(body)
	if len(titleMatches) != 2 {
		return "", nil, nil, fmt.Errorf("linux-hardware monitor title not found")
	}

	title := strings.TrimSpace(titleMatches[1])
	sizePattern := regexp.MustCompile(`(?i)\b(\d+)x(\d+)mm\b`)
	sizeMatches := sizePattern.FindStringSubmatch(title)
	if len(sizeMatches) != 3 {
		return "", nil, nil, fmt.Errorf("linux-hardware monitor size not found")
	}

	widthMM := atoiOrZero(sizeMatches[1])
	heightMM := atoiOrZero(sizeMatches[2])
	if widthMM <= 0 || heightMM <= 0 {
		return "", nil, nil, fmt.Errorf("linux-hardware monitor size invalid")
	}

	canonicalName := strings.TrimSpace(regexp.MustCompile(`(?i)\s+\d+x\d+mm\b.*$`).ReplaceAllString(title, ""))
	widthCM := float64(widthMM) / 10.0
	heightCM := float64(heightMM) / 10.0
	return canonicalName, &widthCM, &heightCM, nil
}

func atoiOrZero(value string) int {
	result := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		result = result*10 + int(r-'0')
	}
	return result
}
