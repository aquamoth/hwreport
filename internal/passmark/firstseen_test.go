package passmark

import "testing"

func TestParseFirstSeen(t *testing.T) {
	body := `<p><strong>Socket:</strong> LGA1150</p>
<p class="alt"><strong class="bg-table-row">CPU First Seen on Charts:</strong> Q2 2013</p>
<p><strong>CPUmark/$Price:</strong> 28.52</p>`
	if got := parseFirstSeen(body); got != "Q2 2013" {
		t.Fatalf("expected Q2 2013, got %q", got)
	}
	if got := parseFirstSeen("<p>nothing here</p>"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
