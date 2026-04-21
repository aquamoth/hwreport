package normalize

import "testing"

func TestMemoryTypeName(t *testing.T) {
	got := MemoryTypeName(26)
	if got == nil || *got != "DDR4" {
		t.Fatalf("expected DDR4, got %#v", got)
	}
}

func TestDiskType(t *testing.T) {
	got := DiskType("Fixed hard disk media", "Samsung SSD 980 PRO", "")
	if got == nil || *got != "ssd" {
		t.Fatalf("expected ssd, got %#v", got)
	}
}

func TestDecodeUint16String(t *testing.T) {
	got := DecodeUint16String([]uint16{'D', 'E', 'L', 'L', 0})
	if got == nil || *got != "DELL" {
		t.Fatalf("unexpected string: %#v", got)
	}
}

func TestDateOnlyFromCIM(t *testing.T) {
	got := DateOnlyFromCIM("20260421110703.000000+120")
	if got == nil || *got != "2026-04-21" {
		t.Fatalf("unexpected date: %#v", got)
	}
}

func TestMatchKey(t *testing.T) {
	disk := NormalizeKey(`SCSI\DISK&VEN_NVME&PROD_SAMSUNG`)
	smart := NormalizeKey(`SCSI\Disk&Ven_NVMe&Prod_Samsung____0001`)
	if !MatchKey(disk, smart) {
		t.Fatalf("expected keys to match")
	}
}
