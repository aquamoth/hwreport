package passmark

import "testing"

func intPtr(v int) *int { return &v }

// Real rows from harddrivebenchmark.net's drive list, including look-alikes the
// matcher must not pick.
var testDriveList = []DriveListEntry{
	{ID: "41138", Name: "WD BLACK SN7100 1TB", DriveMark: intPtr(46894), Samples: 3617},
	{ID: "43207", Name: "NVMe WD BLACK SN7100 2TB", DriveMark: intPtr(55639), Samples: 20},
	{ID: "43608", Name: "NVMe PC SN7100S SanDisk 1024GB", DriveMark: intPtr(53239), Samples: 6},
	{ID: "31762", Name: "WD BLACK SN850X 1TB", DriveMark: intPtr(46511), Samples: 11365},
	{ID: "31895", Name: "WD BLACK SN850X HS 1000GB", DriveMark: intPtr(45269), Samples: 2126},
	{ID: "27229", Name: "Samsung SSD 980 NVMe 500GB", DriveMark: intPtr(18487), Samples: 9278},
	{ID: "27100", Name: "Samsung SSD 980 PRO 500GB", DriveMark: intPtr(35000), Samples: 9000},
	{ID: "38289", Name: "Samsung SSD 980", DriveMark: intPtr(24159), Samples: 147},
	{ID: "22511", Name: "Seagate ST2000DM005", DriveMark: intPtr(737), Samples: 18},
	{ID: "11000", Name: "Seagate ST3000DM001", DriveMark: intPtr(1892), Samples: 130},
	{ID: "30000", Name: "WDC WD10EZEX", DriveMark: intPtr(1221), Samples: 6136},
	{ID: "29620", Name: "WD Blue SN570 1TB", DriveMark: intPtr(24755), Samples: 5543},
	{ID: "30081", Name: "WD Blue SN570 250GB", DriveMark: intPtr(16239), Samples: 393},
	{ID: "40000", Name: "KINGSTON SKC3000S/1024G", DriveMark: intPtr(27105), Samples: 6},
	{ID: "40001", Name: "KINGSTON SKC3000", DriveMark: intPtr(40088), Samples: 11},
	{ID: "29000", Name: "KIOXIA-EXCERIA BASIC SSD", DriveMark: intPtr(44517), Samples: 121},
	{ID: "29001", Name: "KIOXIA-EXCERIA PLUS G3 SSD", DriveMark: intPtr(29564), Samples: 1352},
	{ID: "20000", Name: "WD Blue SA510 2.5 1000GB", DriveMark: intPtr(3455), Samples: 657},
	{ID: "20001", Name: "WD Blue SA510 2.5 2TB", DriveMark: intPtr(3968), Samples: 114},
}

func TestMatchDriveList(t *testing.T) {
	cases := []struct {
		model string
		want  string // "" = no match
	}{
		{"WD_BLACK SN7100 1TB", "WD BLACK SN7100 1TB"},
		{"WD_BLACK SN850X 1000GB", "WD BLACK SN850X 1TB"},
		{"Samsung SSD 980 500GB", "Samsung SSD 980 NVMe 500GB"},
		{"ST2000DM005-2CW102", "Seagate ST2000DM005"},
		{"WDC WD10EZEX-22MFCA0", "WDC WD10EZEX"},
		{"KINGSTON SKC3000S1024G", "KINGSTON SKC3000S/1024G"},
		{"KIOXIA-EXCERIA BASIC SSD", "KIOXIA-EXCERIA BASIC SSD"},
		{"KIOXIA-EXCERIA PLUS G3 SSD", "KIOXIA-EXCERIA PLUS G3 SSD"},
		{"WD Blue SA510 2.5 2TB", "WD Blue SA510 2.5 2TB"},
		// Not in the list, and ST3000DM001 is a different drive.
		{"ST2000DM001-1ER164", ""},
		// Brand only: never guess.
		{"KIOXIA SSD", ""},
	}
	for _, tc := range cases {
		got, ok := matchDriveList(tc.model, testDriveList)
		if tc.want == "" {
			if ok {
				t.Errorf("%q: expected no match, got %q", tc.model, got.Name)
			}
			continue
		}
		if !ok {
			t.Errorf("%q: expected %q, got no match", tc.model, tc.want)
			continue
		}
		if got.Name != tc.want {
			t.Errorf("%q: expected %q, got %q", tc.model, tc.want, got.Name)
		}
	}
}

func TestParseDriveList(t *testing.T) {
	payload := []byte(`{"data":[
		{"id":"41138","name":"WD BLACK SN7100 1TB","size":"931.5 GB","diskmark":"46,894","samples":"3,617","type":"SSD","href":"WD+BLACK+SN7100+1TB&amp;id=41138","output":false},
		{"id":"33933","name":"120G","diskmark":"NA","samples":"8","type":"SSD","href":"120G&amp;id=33933","output":true}
	]}`)
	entries, err := parseDriveList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	first := entries[0]
	if first.DriveMark == nil || *first.DriveMark != 46894 || first.Samples != 3617 {
		t.Errorf("unexpected first entry: %+v", first)
	}
	if first.DetailURL != "https://www.harddrivebenchmark.net/hdd.php?hdd=WD+BLACK+SN7100+1TB&id=41138" {
		t.Errorf("unexpected detail url %q", first.DetailURL)
	}
	if entries[1].DriveMark != nil {
		t.Errorf("NA drive mark should be nil")
	}
	if entries[1].DetailURL != "https://www.harddrivebenchmark.net/hdd_lookup.php?hdd=120G&id=33933" {
		t.Errorf("unexpected lookup url %q", entries[1].DetailURL)
	}
}
