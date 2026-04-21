//go:build windows

package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"specreport/internal/report"
)

func Collect() (*report.Report, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	payload, err := runCollectorScript()
	if err != nil {
		return nil, err
	}

	var out report.Report
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("decode collector output: %w", err)
	}

	out.SchemaVersion = 1
	out.CollectedAtUTC = time.Now().UTC().Format(time.RFC3339)
	out.Hostname = hostname
	return &out, nil
}

func runCollectorScript() ([]byte, error) {
	scriptPath, err := writeCollectorScript()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(scriptPath))

	cmd := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-File",
		scriptPath,
	)

	output, err := cmd.Output()
	if err == nil {
		return output, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr := strings.TrimSpace(string(exitErr.Stderr))
		if stderr != "" {
			return nil, fmt.Errorf("collector script failed: %s", stderr)
		}
	}

	return nil, fmt.Errorf("collector script failed: %w", err)
}

func writeCollectorScript() (string, error) {
	dir, err := os.MkdirTemp("", "hwreport-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	path := filepath.Join(dir, "collector.ps1")
	if err := os.WriteFile(path, []byte(collectorScript), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write collector script: %w", err)
	}
	return path, nil
}

const collectorScript = `
$ErrorActionPreference = 'Stop'

function Convert-DateOnly($value) {
  if ($null -eq $value) { return $null }
  try {
    if ($value -is [datetime]) { return $value.ToString('yyyy-MM-dd') }
    $text = [string]$value
    if ([string]::IsNullOrWhiteSpace($text)) { return $null }
    if ($text.Length -ge 8 -and $text.Substring(0, 8) -match '^\d{8}$') {
      return [datetime]::ParseExact($text.Substring(0, 8), 'yyyyMMdd', $null).ToString('yyyy-MM-dd')
    }
    return ([datetime]$value).ToString('yyyy-MM-dd')
  } catch {
    return $null
  }
}

function Convert-BytesToGB($value) {
  if ($null -eq $value) { return $null }
  $number = [double]$value
  if ($number -le 0) { return $null }
  return [math]::Round($number / 1GB, 2)
}

function Convert-KBToGB($value) {
  if ($null -eq $value) { return $null }
  $number = [double]$value
  if ($number -le 0) { return $null }
  return [math]::Round($number / 1MB, 2)
}

function Convert-ToIntOrNull($value) {
  if ($null -eq $value) { return $null }
  $number = [int]$value
  if ($number -lt 0) { return $null }
  return $number
}

function Normalize-Text($value) {
  if ($null -eq $value) { return $null }
  $text = ([string]$value).Trim()
  if ([string]::IsNullOrWhiteSpace($text)) { return $null }
  return $text
}

function Normalize-Manufacturer($value) {
  $text = Normalize-Text $value
  if (-not $text) { return $null }
  switch ($text) {
    'AuthenticAMD' { return 'AMD' }
    'GenuineIntel' { return 'Intel' }
    '(Standard disk drives)' { return $null }
    default { return $text }
  }
}

function Normalize-Key($value) {
  if ($null -eq $value) { return '' }
  return (([string]$value).ToUpperInvariant() -replace '[^A-Z0-9]', '')
}

function Normalize-MonitorIdentityKey($value) {
  if ($null -eq $value) { return '' }
  $text = [string]$value
  if ($text -match 'DISPLAY[#\\](?<product>[^#\\]+)[#\\](?<instance>[^#\\{]+)') {
    $instance = $matches.instance -replace '_\d+$', ''
    return Normalize-Key ("DISPLAY{0}{1}" -f $matches.product, $instance)
  }
  return Normalize-Key $text
}

function Match-Key($left, $right) {
  if ([string]::IsNullOrWhiteSpace($left) -or [string]::IsNullOrWhiteSpace($right)) { return $false }
  return $left.Contains($right) -or $right.Contains($left)
}

function Decode-Uint16String($values) {
  if ($null -eq $values) { return $null }
  $chars = New-Object System.Collections.Generic.List[char]
  foreach ($value in $values) {
    if ([int]$value -ne 0) {
      [void]$chars.Add([char][int]$value)
    }
  }
  $text = (-join $chars.ToArray()).Trim()
  if ([string]::IsNullOrWhiteSpace($text)) { return $null }
  return $text
}

function Get-UniqueValue($values) {
  $unique = @($values | Where-Object { -not [string]::IsNullOrWhiteSpace($_) } | Sort-Object -Unique)
  if ($unique.Count -eq 1) { return $unique[0] }
  return $null
}

function Get-SlotLabel($deviceLocator, $bankLabel) {
  $device = Normalize-Text $deviceLocator
  $bank = Normalize-Text $bankLabel
  if ($device -and $bank) { return "$device ($bank)" }
  if ($device) { return $device }
  return $bank
}

function Get-DiskType($physicalMediaType, $mediaType, $model, $status) {
  $preferred = Normalize-Text $physicalMediaType
  if ($preferred) {
    switch ($preferred.ToUpperInvariant()) {
      'SSD' { return 'ssd' }
      'SCM' { return 'ssd' }
      'HDD' { return 'hdd' }
      'UNSPECIFIED' { }
      'UNKNOWN' { }
    }
  }
  $joined = ((@($mediaType, $model, $status) -join ' ').ToLowerInvariant())
  if ($joined -match 'ssd|solid state|nvme|emmc') { return 'ssd' }
  if ($joined -match 'hdd|hard disk') { return 'hdd' }
  return $null
}

function Is-RemovableDisk($disk, $physicalDisk) {
  $mediaType = Normalize-Text $disk.MediaType
  if ($mediaType -eq 'Removable Media') { return $true }

  $pnpDeviceId = Normalize-Text $disk.PNPDeviceID
  $model = Normalize-Text $disk.Model
  if ($pnpDeviceId -like 'USBSTOR*' -and $model -like '*USB Device*') { return $true }

  return $false
}

function Get-SmartStatus($physicalDisk, $predictFailure) {
  if ($predictFailure -eq $true) {
    return 'warning'
  }

  if ($physicalDisk) {
    $health = Normalize-Text $physicalDisk.health_status
    switch ($health) {
      'Healthy' { return 'ok' }
      'Warning' { return 'warning' }
      'Unhealthy' { return 'error' }
    }

    $operational = @($physicalDisk.operational_status | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    foreach ($status in $operational) {
      switch ($status) {
        'OK' { return 'ok' }
        'Degraded' { return 'warning' }
        'Predictive Failure' { return 'warning' }
        'Lost Communication' { return 'error' }
        'Error' { return 'error' }
      }
    }
  }

  return $null
}

function Convert-ToRotationDegrees($value) {
  switch ([int]$value) {
    0 { return 0 }
    90 { return 90 }
    180 { return 180 }
    270 { return 270 }
    default { return $null }
  }
}

if (-not ('DisplayInventory' -as [type])) {
Add-Type -Language CSharp -TypeDefinition @"
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;

public sealed class DisplayRotationInfo
{
    public string DeviceName { get; set; }
    public string MonitorId { get; set; }
    public int? RotationDegrees { get; set; }
    public int? PixelWidth { get; set; }
    public int? PixelHeight { get; set; }
}

public static class DisplayInventory
{
    private const int ENUM_CURRENT_SETTINGS = -1;
    private const int EDD_GET_DEVICE_INTERFACE_NAME = 0x00000001;
    private const int DISPLAY_DEVICE_ATTACHED_TO_DESKTOP = 0x00000001;

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct DISPLAY_DEVICE
    {
        public int cb;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string DeviceName;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)]
        public string DeviceString;
        public int StateFlags;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)]
        public string DeviceID;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 128)]
        public string DeviceKey;
    }

    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
    private struct DEVMODE
    {
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string dmDeviceName;
        public short dmSpecVersion;
        public short dmDriverVersion;
        public short dmSize;
        public short dmDriverExtra;
        public int dmFields;
        public int dmPositionX;
        public int dmPositionY;
        public int dmDisplayOrientation;
        public int dmDisplayFixedOutput;
        public short dmColor;
        public short dmDuplex;
        public short dmYResolution;
        public short dmTTOption;
        public short dmCollate;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)]
        public string dmFormName;
        public short dmLogPixels;
        public int dmBitsPerPel;
        public int dmPelsWidth;
        public int dmPelsHeight;
        public int dmDisplayFlags;
        public int dmDisplayFrequency;
        public int dmICMMethod;
        public int dmICMIntent;
        public int dmMediaType;
        public int dmDitherType;
        public int dmReserved1;
        public int dmReserved2;
        public int dmPanningWidth;
        public int dmPanningHeight;
    }

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern bool EnumDisplayDevices(string lpDevice, uint iDevNum, ref DISPLAY_DEVICE lpDisplayDevice, uint dwFlags);

    [DllImport("user32.dll", CharSet = CharSet.Unicode)]
    private static extern bool EnumDisplaySettings(string lpszDeviceName, int iModeNum, ref DEVMODE lpDevMode);

    public static List<DisplayRotationInfo> GetRotations()
    {
        var result = new List<DisplayRotationInfo>();

        for (uint adapterIndex = 0; ; adapterIndex++)
        {
            var adapter = NewDisplayDevice();
            if (!EnumDisplayDevices(null, adapterIndex, ref adapter, 0))
            {
                break;
            }

            if ((adapter.StateFlags & DISPLAY_DEVICE_ATTACHED_TO_DESKTOP) == 0)
            {
                continue;
            }

            var mode = NewDevMode();
            int? rotation = null;
            if (EnumDisplaySettings(adapter.DeviceName, ENUM_CURRENT_SETTINGS, ref mode))
            {
                switch (mode.dmDisplayOrientation)
                {
                    case 0: rotation = 0; break;
                    case 1: rotation = 90; break;
                    case 2: rotation = 180; break;
                    case 3: rotation = 270; break;
                }
            }

            for (uint monitorIndex = 0; ; monitorIndex++)
            {
                var monitor = NewDisplayDevice();
                if (!EnumDisplayDevices(adapter.DeviceName, monitorIndex, ref monitor, EDD_GET_DEVICE_INTERFACE_NAME))
                {
                    break;
                }

                result.Add(new DisplayRotationInfo
                {
                    DeviceName = adapter.DeviceName,
                    MonitorId = monitor.DeviceID,
                    RotationDegrees = rotation,
                    PixelWidth = mode.dmPelsWidth,
                    PixelHeight = mode.dmPelsHeight
                });
            }
        }

        return result;
    }

    private static DISPLAY_DEVICE NewDisplayDevice()
    {
        var value = new DISPLAY_DEVICE();
        value.cb = Marshal.SizeOf(typeof(DISPLAY_DEVICE));
        return value;
    }

    private static DEVMODE NewDevMode()
    {
        var value = new DEVMODE();
        value.dmSize = (short)Marshal.SizeOf(typeof(DEVMODE));
        return value;
    }
}
"@
}

$computerSystem = Get-CimInstance Win32_ComputerSystem
$computerSystemProduct = Get-CimInstance Win32_ComputerSystemProduct -ErrorAction SilentlyContinue | Select-Object -First 1
$operatingSystem = Get-CimInstance Win32_OperatingSystem
$processor = Get-CimInstance Win32_Processor | Select-Object -First 1
$memoryModulesRaw = @(Get-CimInstance Win32_PhysicalMemory)
$memoryArraysRaw = @(Get-CimInstance Win32_PhysicalMemoryArray -ErrorAction SilentlyContinue)
$disksRaw = @(Get-CimInstance Win32_DiskDrive)
$physicalMediaRaw = @(Get-CimInstance Win32_PhysicalMedia -ErrorAction SilentlyContinue)
$physicalDisksRaw = @(Get-PhysicalDisk -ErrorAction SilentlyContinue)
$smartRaw = @(Get-CimInstance -Namespace 'root\wmi' -ClassName MSStorageDriver_FailurePredictStatus -ErrorAction SilentlyContinue)
$gpusRaw = @(Get-CimInstance Win32_VideoController)
$desktopMonitorsRaw = @(Get-CimInstance Win32_DesktopMonitor -ErrorAction SilentlyContinue)
$monitorIDsRaw = @(Get-CimInstance -Namespace 'root\wmi' -ClassName WmiMonitorID -Filter 'Active = TRUE' -ErrorAction SilentlyContinue)
$monitorParamsRaw = @(Get-CimInstance -Namespace 'root\wmi' -ClassName WmiMonitorBasicDisplayParams -Filter 'Active = TRUE' -ErrorAction SilentlyContinue)

$memoryTypeMap = @{
  20 = 'DDR'
  21 = 'DDR2'
  24 = 'DDR3'
  26 = 'DDR4'
  34 = 'DDR5'
}

$physicalMediaByTag = @{}
foreach ($item in $physicalMediaRaw) {
  $physicalMediaByTag[(Normalize-Key $item.Tag)] = $item
}

$smartByKey = @{}
foreach ($item in $smartRaw) {
  $smartByKey[(Normalize-Key $item.InstanceName)] = $(if ($item.PredictFailure) { 'warning' } else { 'ok' })
}

$physicalDisks = foreach ($item in $physicalDisksRaw) {
  [ordered]@{
    key = Normalize-Key $item.FriendlyName
    manufacturer = Normalize-Manufacturer $item.Manufacturer
    media_type = Normalize-Text $item.MediaType
    bus_type = Normalize-Text $item.BusType
    health_status = Normalize-Text $item.HealthStatus
    operational_status = @($item.OperationalStatus | ForEach-Object { Normalize-Text $_ } | Where-Object { $_ })
    size = if ($null -ne $item.Size) { [uint64]$item.Size } else { $null }
  }
}

$desktopMonitors = foreach ($item in $desktopMonitorsRaw) {
  $key = Normalize-MonitorIdentityKey $item.PNPDeviceID
  if (-not $key) { continue }
  [ordered]@{
    key = $key
    manufacturer = Normalize-Text $item.MonitorManufacturer
    name = Normalize-Text $item.Name
    pixel_width = if ($null -ne $item.ScreenWidth) { [uint32]$item.ScreenWidth } else { $null }
    pixel_height = if ($null -ne $item.ScreenHeight) { [uint32]$item.ScreenHeight } else { $null }
  }
}

$displayByKey = @{}
foreach ($item in [DisplayInventory]::GetRotations()) {
  $monitorKey = Normalize-MonitorIdentityKey $item.MonitorId
  if (-not $monitorKey) { continue }
  $displayByKey[$monitorKey] = [ordered]@{
    rotation_degrees = Convert-ToRotationDegrees $item.RotationDegrees
    pixel_width = if ($null -ne $item.PixelWidth -and [int]$item.PixelWidth -gt 0) { [uint32]$item.PixelWidth } else { $null }
    pixel_height = if ($null -ne $item.PixelHeight -and [int]$item.PixelHeight -gt 0) { [uint32]$item.PixelHeight } else { $null }
  }
}

$monitorParamsByKey = @{}
foreach ($item in $monitorParamsRaw) {
  $key = Normalize-MonitorIdentityKey $item.InstanceName
  if (-not $key) { continue }
  $monitorParamsByKey[$key] = [ordered]@{
    physical_width = if ($null -ne $item.MaxHorizontalImageSize -and [int]$item.MaxHorizontalImageSize -gt 0) { [double]$item.MaxHorizontalImageSize } else { $null }
    physical_height = if ($null -ne $item.MaxVerticalImageSize -and [int]$item.MaxVerticalImageSize -gt 0) { [double]$item.MaxVerticalImageSize } else { $null }
    physical_unit = if (($null -ne $item.MaxHorizontalImageSize -and [int]$item.MaxHorizontalImageSize -gt 0) -or ($null -ne $item.MaxVerticalImageSize -and [int]$item.MaxVerticalImageSize -gt 0)) { 'cm' } else { $null }
  }
}

$memoryModules = foreach ($item in $memoryModulesRaw) {
  $memoryType = $null
  if ($memoryTypeMap.ContainsKey([int]$item.SMBIOSMemoryType)) {
    $memoryType = $memoryTypeMap[[int]$item.SMBIOSMemoryType]
  }
  [ordered]@{
    manufacturer = Normalize-Text $item.Manufacturer
    part_number = Normalize-Text $item.PartNumber
    type = $memoryType
    size_gb = Convert-BytesToGB $item.Capacity
    slot = Get-SlotLabel $item.DeviceLocator $item.BankLabel
  }
}

$memoryTotalBytes = ($memoryModulesRaw | Measure-Object -Property Capacity -Sum).Sum
if (-not $memoryTotalBytes -and $computerSystem.TotalPhysicalMemory) {
  $memoryTotalBytes = $computerSystem.TotalPhysicalMemory
}
$memoryTotalSlots = (($memoryArraysRaw | Measure-Object -Property MemoryDevices -Sum).Sum)
if ($null -eq $memoryTotalSlots) { $memoryTotalSlots = 0 }
$memoryInstalledSlots = @($memoryModules).Count
$memoryEmptySlots = [math]::Max([int]$memoryTotalSlots - [int]$memoryInstalledSlots, 0)

$storage = @(
foreach ($disk in $disksRaw) {
  $deviceKey = Normalize-Key $disk.DeviceID
  $pnpKey = Normalize-Key $disk.PNPDeviceID
  $media = $physicalMediaByTag[$deviceKey]
  $modelKey = Normalize-Key $disk.Model

  $physicalDisk = $null
  foreach ($candidate in $physicalDisks) {
    if ((Match-Key $candidate.key $modelKey) -or ($candidate.size -and $disk.Size -and [uint64]$candidate.size -eq [uint64]$disk.Size)) {
      $physicalDisk = $candidate
      break
    }
  }

  if (Is-RemovableDisk $disk $physicalDisk) {
    continue
  }

  $predictFailure = $null
  foreach ($entry in $smartByKey.GetEnumerator()) {
    if (Match-Key $entry.Key $pnpKey -or Match-Key $entry.Key $deviceKey) {
      $predictFailure = ($entry.Value -eq 'warning')
      break
    }
  }
  $smartStatus = Get-SmartStatus $physicalDisk $predictFailure

  [ordered]@{
    manufacturer = Normalize-Manufacturer $(if ($physicalDisk -and $physicalDisk.manufacturer) { $physicalDisk.manufacturer } elseif ($disk.Manufacturer) { $disk.Manufacturer } elseif ($media) { $media.Manufacturer } else { $null })
    model = Normalize-Text $disk.Model
    type = Get-DiskType $(if ($physicalDisk) { $physicalDisk.media_type } else { $null }) $disk.MediaType $disk.Model $disk.Status
    size_gb = Convert-BytesToGB $disk.Size
    manufacture_date = $(if ($media) { Convert-DateOnly $media.ManufactureDate } else { $null })
    smart_status = $smartStatus
  }
}
) | Sort-Object model

$gpuSeen = @{}
$gpus = foreach ($gpu in $gpusRaw) {
  $name = Normalize-Text $gpu.Name
  if (-not $name) { continue }
  $key = Normalize-Key $name
  if ($gpuSeen.ContainsKey($key)) { continue }
  $gpuSeen[$key] = $true
  [ordered]@{
    manufacturer = Normalize-Text $gpu.AdapterCompatibility
    model = $name
  }
}

$monitorSeen = @{}
$monitors = @(
foreach ($monitor in $monitorIDsRaw) {
  $instanceKey = Normalize-MonitorIdentityKey $monitor.InstanceName
  if ($monitorSeen.ContainsKey($instanceKey)) {
    continue
  }
  $desktop = $null
  foreach ($candidate in $desktopMonitors) {
    if (Match-Key $instanceKey $candidate.key) {
      $desktop = $candidate
      break
    }
  }
  $params = $monitorParamsByKey[$instanceKey]
  $displayInfo = $null
  foreach ($entry in $displayByKey.GetEnumerator()) {
    if (Match-Key $entry.Key $instanceKey) {
      $displayInfo = $entry.Value
      break
    }
  }

  $manufacturer = Decode-Uint16String $monitor.ManufacturerName
  if (-not $manufacturer -and $desktop) { $manufacturer = $desktop.manufacturer }
  $model = Decode-Uint16String $monitor.UserFriendlyName
  if (-not $model -and $desktop) { $model = $desktop.name }
  $dedupeKey = Normalize-Key ((@($instanceKey, $manufacturer, $model) -join '|'))
  if ($monitorSeen.ContainsKey($dedupeKey)) {
    continue
  }
  $monitorSeen[$instanceKey] = $true
  $monitorSeen[$dedupeKey] = $true

  [ordered]@{
    manufacturer = $manufacturer
    model = $model
    pixel_width = $(if ($displayInfo) { $displayInfo.pixel_width } elseif ($desktop) { $desktop.pixel_width } else { $null })
    pixel_height = $(if ($displayInfo) { $displayInfo.pixel_height } elseif ($desktop) { $desktop.pixel_height } else { $null })
    physical_width = $(if ($params) { $params.physical_width } else { $null })
    physical_height = $(if ($params) { $params.physical_height } else { $null })
    physical_unit = $(if ($params) { $params.physical_unit } else { $null })
    rotation_degrees = $(if ($displayInfo) { $displayInfo.rotation_degrees } else { $null })
  }
}
) | Sort-Object model

$computerFirstUse = Convert-DateOnly $computerSystem.InstallDate
if (-not $computerFirstUse -and $computerSystemProduct) {
  $computerFirstUse = Convert-DateOnly $computerSystemProduct.InstallDate
}

$report = [ordered]@{
  computer = [ordered]@{
    manufacturer = Normalize-Manufacturer $computerSystem.Manufacturer
    model = Normalize-Text $computerSystem.Model
    first_use_date = $computerFirstUse
  }
  os = [ordered]@{
    name = Normalize-Text (($operatingSystem.Caption -replace '^Microsoft\s+', ''))
    version = Normalize-Text $operatingSystem.Version
    first_install_date = Convert-DateOnly $operatingSystem.InstallDate
  }
  cpu = [ordered]@{
    manufacturer = Normalize-Manufacturer $processor.Manufacturer
    model = Normalize-Text $processor.Name
  }
  memory = [ordered]@{
    manufacturer = Get-UniqueValue ($memoryModules | ForEach-Object { $_.manufacturer })
    model = Get-UniqueValue ($memoryModules | ForEach-Object { $_.part_number })
    type = Get-UniqueValue ($memoryModules | ForEach-Object { $_.type })
    total_installed_gb = Convert-BytesToGB $memoryTotalBytes
    total_slots = Convert-ToIntOrNull $memoryTotalSlots
    empty_slots = Convert-ToIntOrNull $memoryEmptySlots
    empty_slot_locations = @()
    free_gb = Convert-KBToGB $operatingSystem.FreePhysicalMemory
    modules = @($memoryModules)
  }
  storage = @($storage)
  gpu = @($gpus)
  monitors = @($monitors)
}

$report | ConvertTo-Json -Depth 6 -Compress
`
