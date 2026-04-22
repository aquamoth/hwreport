//go:build integration

package passmark

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriveLookupIntegrationKnownModels(t *testing.T) {
	testCases := []struct {
		query                   string
		expectedCanonicalPieces []string
	}{
		{
			query:                   "HS-SSD-WAVE(S) 1024G",
			expectedCanonicalPieces: []string{"1024"},
		},
		{
			query:                   "KINGSTON SKC3000S1024G",
			expectedCanonicalPieces: []string{"SKC3000S", "1024"},
		},
		{
			query:                   "Micron MTFDKBA512TGW-2BP15ABLT",
			expectedCanonicalPieces: []string{"MTFDKBA512TGW"},
		},
		{
			query:                   "NVMe CL4-3D256-Q11 NVMe SSSTC 256GB",
			expectedCanonicalPieces: []string{"256"},
		},
		{
			query:                   "NVMe SAMSUNG MZVL8512HELU-00BH1",
			expectedCanonicalPieces: []string{"MZVL8512HELU"},
		},
		{
			query:                   "NVMe WD PC SN740 SDDQNQD-512G-1014",
			expectedCanonicalPieces: []string{"SN740", "512"},
		},
		{
			query:                   "SAMSUNG MZVL8512HELU-00BTW",
			expectedCanonicalPieces: []string{"MZVL8512HELU"},
		},
		{
			query:                   "ST1000DM010-2EP102",
			expectedCanonicalPieces: []string{"ST1000DM010"},
		},
		{
			query:                   "WD Green 2.5 1000GB",
			expectedCanonicalPieces: []string{"WD", "GREEN", "1000"},
		},
	}

	cachePath := filepath.Join(t.TempDir(), "drive-cache.json")
	client, err := NewDriveClient(cachePath)
	if err != nil {
		t.Fatalf("NewDriveClient returned error: %v", err)
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.query, func(t *testing.T) {
			result, err := client.Lookup(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("Lookup(%q) returned error: %v", tc.query, err)
			}
			if result.DriveMark == nil {
				t.Fatalf("Lookup(%q) returned nil DriveMark", tc.query)
			}
			if strings.TrimSpace(result.CanonicalName) == "" {
				t.Fatalf("Lookup(%q) returned empty CanonicalName", tc.query)
			}
			if strings.TrimSpace(result.LookupURL) == "" {
				t.Fatalf("Lookup(%q) returned empty LookupURL", tc.query)
			}

			canonical := strings.ToUpper(result.CanonicalName)
			for _, piece := range tc.expectedCanonicalPieces {
				if !strings.Contains(canonical, strings.ToUpper(piece)) {
					t.Fatalf("Lookup(%q) canonical name %q does not contain expected piece %q", tc.query, result.CanonicalName, piece)
				}
			}
		})
	}
}
