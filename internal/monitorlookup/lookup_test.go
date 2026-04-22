package monitorlookup

import "testing"

func TestParseLinuxHardwareMonitor(t *testing.T) {
	body := `
<html><body>
<h2>Device 'BOE NE135A1M-NY1 BOE0CB4 2880x1920 285x190mm 13.5-inch'</h2>
</body></html>`

	name, width, height, err := parseLinuxHardwareMonitor(body)
	if err != nil {
		t.Fatalf("parseLinuxHardwareMonitor returned error: %v", err)
	}
	if name != "BOE NE135A1M-NY1 BOE0CB4 2880x1920" {
		t.Fatalf("unexpected name %q", name)
	}
	if width == nil || *width != 28.5 {
		t.Fatalf("unexpected width %#v", width)
	}
	if height == nil || *height != 19 {
		t.Fatalf("unexpected height %#v", height)
	}
}

func TestNormalizePNPID(t *testing.T) {
	if value := normalizePNPID(" boe0cb4 "); value != "BOE0CB4" {
		t.Fatalf("unexpected normalized pnp id %q", value)
	}
	if value := normalizePNPID("BOE-0CB4"); value != "" {
		t.Fatalf("expected invalid pnp id to be rejected, got %q", value)
	}
}
