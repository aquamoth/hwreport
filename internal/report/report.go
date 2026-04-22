package report

type Report struct {
	SchemaVersion  int       `json:"schema_version"`
	CollectedAtUTC string    `json:"collected_at_utc"`
	Hostname       string    `json:"hostname"`
	LoggedInUser   *string   `json:"logged_in_user"`
	Computer       Computer  `json:"computer"`
	OS             OS        `json:"os"`
	CPU            CPU       `json:"cpu"`
	Memory         Memory    `json:"memory"`
	Storage        []Drive   `json:"storage"`
	GPU            []GPU     `json:"gpu"`
	Monitors       []Monitor `json:"monitors"`
}

type Computer struct {
	Manufacturer *string `json:"manufacturer"`
	Model        *string `json:"model"`
	FirstUseDate *string `json:"first_use_date"`
}

type OS struct {
	Name             *string `json:"name"`
	Version          *string `json:"version"`
	FirstInstallDate *string `json:"first_install_date"`
}

type CPU struct {
	Manufacturer *string `json:"manufacturer"`
	Model        *string `json:"model"`
}

type Memory struct {
	Manufacturer       *string        `json:"manufacturer"`
	Model              *string        `json:"model"`
	Type               *string        `json:"type"`
	ConfiguredSpeedMHz *int           `json:"configured_speed_mhz"`
	RatedSpeedMHz      *int           `json:"rated_speed_mhz"`
	TotalInstalledGB   *float64       `json:"total_installed_gb"`
	TotalSlots         *int           `json:"total_slots"`
	EmptySlots         *int           `json:"empty_slots"`
	EmptySlotLocations []string       `json:"empty_slot_locations"`
	FreeGB             *float64       `json:"free_gb"`
	Modules            []MemoryModule `json:"modules"`
}

type MemoryModule struct {
	Manufacturer       *string  `json:"manufacturer"`
	PartNumber         *string  `json:"part_number"`
	Type               *string  `json:"type"`
	ConfiguredSpeedMHz *int     `json:"configured_speed_mhz"`
	RatedSpeedMHz      *int     `json:"rated_speed_mhz"`
	SizeGB             *float64 `json:"size_gb"`
	Slot               *string  `json:"slot"`
}

type Drive struct {
	Manufacturer    *string         `json:"manufacturer"`
	Model           *string         `json:"model"`
	Type            *string         `json:"type"`
	SizeGB          *float64        `json:"size_gb"`
	ManufactureDate *string         `json:"manufacture_date"`
	SmartStatus     *string         `json:"smart_status"`
	Benchmark       *DriveBenchmark `json:"benchmark,omitempty"`
}

type DriveBenchmark struct {
	CanonicalName       *string  `json:"canonical_name"`
	DriveMark           *int     `json:"drive_mark"`
	SequentialReadMBps  *float64 `json:"sequential_read_mbps"`
	SequentialWriteMBps *float64 `json:"sequential_write_mbps"`
	RandomReadWriteMBps *float64 `json:"random_read_write_mbps"`
	IOPS4KQD1MBps       *float64 `json:"iops_4kqd1_mbps"`
	LookupURL           *string  `json:"lookup_url"`
}

type GPU struct {
	Manufacturer *string `json:"manufacturer"`
	Model        *string `json:"model"`
}

type Monitor struct {
	Manufacturer    *string  `json:"manufacturer"`
	Model           *string  `json:"model"`
	EdidPNPID       *string  `json:"edid_pnp_id,omitempty"`
	EdidDisplayName *string  `json:"edid_display_name,omitempty"`
	PixelWidth      *uint32  `json:"pixel_width"`
	PixelHeight     *uint32  `json:"pixel_height"`
	PhysicalWidth   *float64 `json:"physical_width"`
	PhysicalHeight  *float64 `json:"physical_height"`
	PhysicalUnit    *string  `json:"physical_unit"`
	PhysicalSource  *string  `json:"physical_source,omitempty"`
	RotationDegrees *int     `json:"rotation_degrees"`
}
