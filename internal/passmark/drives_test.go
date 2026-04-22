package passmark

import "testing"

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

	detailURL, driveName, driveMark, err := parseDriveLookupEntry(body, id)
	if err != nil {
		t.Fatalf("parseDriveLookupEntry returned error: %v", err)
	}
	if detailURL != "https://www.harddrivebenchmark.net/hdd.php?hdd=QEMU%20QEMU%20HARDDISK&id=30967" {
		t.Fatalf("unexpected detailURL %q", detailURL)
	}
	if driveName != "QEMU QEMU HARDDISK" {
		t.Fatalf("unexpected driveName %q", driveName)
	}
	if driveMark == nil || *driveMark != 10035 {
		t.Fatalf("unexpected driveMark %#v", driveMark)
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

func assertFloat(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil || *value != expected {
		t.Fatalf("unexpected float value %#v, expected %v", value, expected)
	}
}
