# hwreport

`hwreport` is a standalone Windows hardware inventory tool intended to run directly from a USB stick. It collects a minimal JSON report about the current machine without requiring installation or user interaction.

The binary is written in Go. The executable can be copied to removable media and run on Windows 11 computers to produce per-machine report files for later aggregation.

## What It Collects

- Computer manufacturer and model
- Computer first-use date when Windows exposes one
- OS name, version, and first install date
- CPU manufacturer and model
- Memory manufacturer, model, type, installed capacity, free memory, populated slots, and empty slot count
- Storage devices, size, type, and best-effort SMART status
- GPU manufacturer and model
- Active monitors, pixel and physical size when available, and current rotation

## Usage

Run with the default output filename:

```powershell
.\hwreport.exe
```

Run with an explicit output path:

```powershell
.\hwreport.exe --out E:\reports\office-pc.json
```

Show help:

```powershell
.\hwreport.exe --help
```

Print version information:

```powershell
.\hwreport.exe --version
```

## Output Files

If `--out` is omitted, the tool writes a JSON file in the current directory using this pattern:

```text
HOSTNAME-YYYY-MM-DD.json
```

Existing files are never overwritten. If the target name already exists, the tool appends an index:

```text
HOSTNAME-YYYY-MM-DD-1.json
HOSTNAME-YYYY-MM-DD-2.json
```

## Building

Requirements:

- Go 1.26 or newer
- Git, to compute the build version and commit metadata

Build the latest checked-out version with the provided script:

```powershell
pwsh -File .\build.ps1
```

The script builds `hwreport.exe` in the repository root and embeds:

- the semantic version derived from `VERSION` plus git commit height
- the current commit hash

## Versioning

The version format is:

```text
major.minor.patch
```

`major.minor` is stored in the root [VERSION](/abs/path/c:/source/specreport/VERSION) file.

`patch` is calculated by the build script as the number of commits since the last commit that changed `VERSION`, inclusive of that change commit. In practice:

- change `VERSION` from `1.0` to `1.1`
- commit that change
- the first build from that commit becomes `1.1.1`
- each later commit increments the patch number automatically

This is intentionally similar to version-height schemes such as NerdBank.GitVersion, but implemented locally for this repository.

## Notes

- The tool targets Windows 11.
- Some hardware details depend on what Windows exposes through WMI, CIM, and display APIs.
- Monitor rotation is reported using standard Windows values: `0`, `90`, `180`, or `270`.
- Disk manufacture dates and empty memory slot locations are often unavailable on typical systems.
