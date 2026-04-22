package passmark

import (
	"strings"
	"testing"
)

func TestParseDriveLookupEntry(t *testing.T) {
	body := `
<link rel="canonical" href="https://www.harddrivebenchmark.net/hdd_lookup.php?id=30967&amp;hdd=QEMU+QEMU+HARDDISK">
<li id="pk30967">
  <span class="more_details">
    <a class="name" href="/hdd.php?hdd=QEMU%20QEMU%20HARDDISK&amp;id=30967"></a>
  </span>
  <a href="/hdd.php?hdd=QEMU%20QEMU%20HARDDISK&amp;id=30967">
    <span class="prdname">QEMU QEMU HARDDISK</span>
    <span class="mark-neww">10,035</span>
  </a>
</li>`

	id, canonicalName, canonicalURL, err := parseDriveLookupCanonical(body)
	if err != nil {
		t.Fatalf("parseDriveLookupCanonical returned error: %v", err)
	}
	if id != "30967" {
		t.Fatalf("unexpected id %q", id)
	}
	if canonicalName != "QEMU QEMU HARDDISK" {
		t.Fatalf("unexpected canonicalName %q", canonicalName)
	}
	if canonicalURL != "https://www.harddrivebenchmark.net/hdd_lookup.php?id=30967&hdd=QEMU+QEMU+HARDDISK" {
		t.Fatalf("unexpected canonicalURL %q", canonicalURL)
	}

	entry, err := chooseDriveLookupEntry("QEMU QEMU HARDDISK", body, id)
	if err != nil {
		t.Fatalf("chooseDriveLookupEntry returned error: %v", err)
	}
	if entry.DetailURL != "https://www.harddrivebenchmark.net/hdd.php?hdd=QEMU%20QEMU%20HARDDISK&id=30967" {
		t.Fatalf("unexpected detailURL %q", entry.DetailURL)
	}
	if entry.DriveName != "QEMU QEMU HARDDISK" {
		t.Fatalf("unexpected driveName %q", entry.DriveName)
	}
	if entry.DriveMark == nil || *entry.DriveMark != 10035 {
		t.Fatalf("unexpected driveMark %#v", entry.DriveMark)
	}
}

func TestParseDriveDetailMetrics(t *testing.T) {
	body := `
<link rel="canonical" href="https://www.harddrivebenchmark.net/hdd.php?hdd=HARDDISK&amp;id=33581">
<div class="right-desc" style="text-align: center">
  <div class="right-header"><span>Average Drive Rating</span></div>
  <span style="font-family: Arial, Helvetica, sans-serif;font-size: 44px; font-weight: bold; color: #F48A18;">2570</span><br>
</div>
<h2>Disk Test Suite Average Results for HARDDISK</h2>
<table id="test-suite-results" class="table">
  <tr><th>Sequential Read</th><td>243 MBytes/Sec</td></tr>
  <tr class="bg-table-row"><th>Sequential Write</th><td>251 MBytes/Sec</td></tr>
  <tr><th>Random Seek Read Write (IOPS 32KQD20)</th><td>206 MBytes/Sec</td></tr>
  <tr class="bg-table-row"><th>IOPS 4KQD1</th><td>23 MBytes/Sec</td></tr>
</table>`

	canonicalName, canonicalURL, err := parseDriveDetailCanonical(body)
	if err != nil {
		t.Fatalf("parseDriveDetailCanonical returned error: %v", err)
	}
	if canonicalName != "HARDDISK" {
		t.Fatalf("unexpected canonicalName %q", canonicalName)
	}
	if canonicalURL != "https://www.harddrivebenchmark.net/hdd.php?hdd=HARDDISK&id=33581" {
		t.Fatalf("unexpected canonicalURL %q", canonicalURL)
	}

	metrics, err := parseDriveDetailMetrics(body)
	if err != nil {
		t.Fatalf("parseDriveDetailMetrics returned error: %v", err)
	}
	if metrics.DriveMark == nil || *metrics.DriveMark != 2570 {
		t.Fatalf("unexpected drive mark %#v", metrics.DriveMark)
	}
	assertFloat(t, metrics.SequentialReadMBps, 243)
	assertFloat(t, metrics.SequentialWriteMBps, 251)
	assertFloat(t, metrics.RandomReadWriteMBps, 206)
	assertFloat(t, metrics.IOPS4KQD1MBps, 23)
}

func TestDriveLookupCandidatesAddCapacityVariants(t *testing.T) {
	candidates := driveLookupCandidates("KINGSTON SKC3000S1024G")
	joined := "\n" + strings.Join(candidates, "\n") + "\n"

	for _, expected := range []string{
		"KINGSTON SKC3000S1024G",
		"KINGSTON SKC3000S/1024G",
		"KINGSTON SKC3000S 1024G",
		"SKC3000S1024G",
		"SKC3000S/1024G",
		"SKC3000S",
	} {
		if !strings.Contains(joined, "\n"+expected+"\n") {
			t.Fatalf("expected candidate %q in %v", expected, candidates)
		}
	}
}

func TestDriveLookupCandidatesTrimRevisionSuffixes(t *testing.T) {
	candidates := driveLookupCandidates("Micron MTFDKBA512TGW-2BP15ABLT")
	joined := "\n" + strings.Join(candidates, "\n") + "\n"

	for _, expected := range []string{
		"Micron MTFDKBA512TGW-2BP15ABLT",
		"MTFDKBA512TGW-2BP15ABLT",
		"MTFDKBA512TGW",
	} {
		if !strings.Contains(joined, "\n"+expected+"\n") {
			t.Fatalf("expected candidate %q in %v", expected, candidates)
		}
	}
}

func TestChooseDriveLookupEntryPrefersStrongPartialMatch(t *testing.T) {
	body := `
<link rel="canonical" href="https://www.harddrivebenchmark.net/hdd_lookup.php?id=41007&amp;hdd=MTFDKBA512QGN-1BN1AABGA">
<li id="pk41007">
  <a href="/hdd.php?hdd=MTFDKBA512QGN-1BN1AABGA&amp;id=41007">
    <span class="prdname">MTFDKBA512QGN-1BN1AABGA</span>
    <span class="mark-neww">37250</span>
  </a>
</li>
<li id="pk45734">
  <a href="/hdd.php?hdd=MTFDKBA512TGW-1BP1AABHA&amp;id=45734">
    <span class="prdname">MTFDKBA512TGW-1BP1AABHA</span>
    <span class="mark-neww">37278</span>
  </a>
</li>`

	entry, err := chooseDriveLookupEntry("MTFDKBA512TGW", body, "41007")
	if err != nil {
		t.Fatalf("chooseDriveLookupEntry returned error: %v", err)
	}
	if entry.ID != "45734" {
		t.Fatalf("expected best match id 45734, got %q", entry.ID)
	}
	if entry.DriveName != "MTFDKBA512TGW-1BP1AABHA" {
		t.Fatalf("unexpected driveName %q", entry.DriveName)
	}
}

func TestChooseDriveLookupEntryWithoutCanonicalTag(t *testing.T) {
	body := `
<li id="pk123">
  <a href="/hdd.php?hdd=ST1000DM010&amp;id=123">
    <span class="prdname">ST1000DM010</span>
    <span class="mark-neww">1363</span>
  </a>
</li>
<li id="pk124">
  <a href="/hdd.php?hdd=ST1000DM003&amp;id=124">
    <span class="prdname">ST1000DM003</span>
    <span class="mark-neww">1172</span>
  </a>
</li>`

	entry, err := chooseDriveLookupEntry("ST1000DM010-2EP102", body, "")
	if err != nil {
		t.Fatalf("chooseDriveLookupEntry returned error: %v", err)
	}
	if entry.ID != "123" {
		t.Fatalf("expected best match id 123, got %q", entry.ID)
	}
	if entry.DriveName != "ST1000DM010" {
		t.Fatalf("unexpected driveName %q", entry.DriveName)
	}
}

func TestParseDriveLookupTableEntries(t *testing.T) {
	body := `
<tr><td><a href="/hdd_lookup.php?hdd=ST1000DM010&amp;id=123">ST1000DM010</a></td><td>931.5 GB</td><td>1,363</td><td>44</td><td>NA</td><td>NA</td></tr>
<tr><td><a href="/hdd_lookup.php?hdd=MTFDKBA512TGW-1BP1AABHA&amp;id=45734">MTFDKBA512TGW-1BP1AABHA</a></td><td>476.9 GB</td><td>37,278</td><td>987</td><td>NA</td><td>NA</td></tr>`

	entries, err := parseDriveLookupEntries(body)
	if err != nil {
		t.Fatalf("parseDriveLookupEntries returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].ID != "123" || entries[0].DriveName != "ST1000DM010" {
		t.Fatalf("unexpected first entry %#v", entries[0])
	}
	if entries[1].ID != "45734" || entries[1].DriveName != "MTFDKBA512TGW-1BP1AABHA" {
		t.Fatalf("unexpected second entry %#v", entries[1])
	}
}

func TestParseDriveSearchEntries(t *testing.T) {
	body := `
<div class="result_title"><b>1.</b>&nbsp;<a href="https://www.harddrivebenchmark.net/hdd.php?hdd=MTFDKBA512TGW-1BP1AABHA&#38;id=45734" >MTFDKBA512TGW&#45;1BP1AABHA &#45; Benchmark performance</a><span class="category"> [Benchmark results]</span></div>
<div class="result_title"><b>2.</b>&nbsp;<a href="https://www.harddrivebenchmark.net/hdd_lookup.php?hdd=WD+Green+2.5+1000GB&#38;id=30438" >WD Green 2.5 1000GB &#45; Benchmark performance</a></div>`

	entries, err := parseDriveSearchEntries(body)
	if err != nil {
		t.Fatalf("parseDriveSearchEntries returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].DetailURL != "https://www.harddrivebenchmark.net/hdd.php?hdd=MTFDKBA512TGW-1BP1AABHA&id=45734" {
		t.Fatalf("unexpected first detailURL %q", entries[0].DetailURL)
	}
	if entries[0].DriveName != "MTFDKBA512TGW-1BP1AABHA" {
		t.Fatalf("unexpected first driveName %q", entries[0].DriveName)
	}
	if entries[1].DriveName != "WD Green 2.5 1000GB" {
		t.Fatalf("unexpected second driveName %q", entries[1].DriveName)
	}
}

func TestHasStrongDriveIdentifierMatch(t *testing.T) {
	testCases := []struct {
		query     string
		candidate string
		expected  bool
	}{
		{
			query:     "KINGSTON SKC3000S1024G",
			candidate: "KINGSTON SKC3000S/1024G",
			expected:  true,
		},
		{
			query:     "KINGSTON SKC3000S1024G",
			candidate: "KINGSTON SKC2000M8500G",
			expected:  false,
		},
		{
			query:     "Micron MTFDKBA512TGW-2BP15ABLT",
			candidate: "MTFDKBA512TGW-1BP1AABHA",
			expected:  true,
		},
		{
			query:     "Micron MTFDKBA512TGW-2BP15ABLT",
			candidate: "Micron MTFDKBA1T0QGN-1BN1AABLT",
			expected:  false,
		},
		{
			query:     "WD Green 2.5 1000GB",
			candidate: "WD Green 2.5 1000GB",
			expected:  true,
		},
	}

	for _, tc := range testCases {
		if actual := hasStrongDriveIdentifierMatch(tc.query, tc.candidate); actual != tc.expected {
			t.Fatalf("hasStrongDriveIdentifierMatch(%q, %q) = %v, expected %v", tc.query, tc.candidate, actual, tc.expected)
		}
	}
}

func assertFloat(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("unexpected float value %#v, expected %v", value, expected)
	}
}
