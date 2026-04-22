# hwreport

`hwreport` is a small toolkit for collecting and summarizing Windows hardware inventory data.

It currently contains two standalone Windows executables:

- `hwreport.exe` collects a JSON report from a single computer
- `hwoverview.exe` aggregates many report JSON files into an HTML overview table

The binary is written in Go. The executable can be copied to removable media and run on Windows 11 computers to produce per-machine report files for later aggregation.

## hwreport: What It Collects

- Computer manufacturer and model
- Computer first-use date when Windows exposes one
- Currently logged-in console user, when one is present
- OS name, version, and first install date
- CPU manufacturer and model
- Memory manufacturer, model, type, installed capacity, free memory, populated slots, and empty slot count
- Storage devices, size, type, and best-effort SMART status
- GPU manufacturer and model
- Active monitors, pixel and physical size when available, and current rotation

## hwreport: Usage

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

## hwreport: Output Files

## hwoverview: Usage

Aggregate all report JSON files in the current directory:

```powershell
.\hwoverview.exe
```

Aggregate reports from a specific directory:

```powershell
.\hwoverview.exe --in D:\hwreport
```

Write the HTML overview to a specific file:

```powershell
.\hwoverview.exe --in D:\hwreport --out D:\hwreport\overview.html
```

The overview is written as HTML.

Each row includes:

- computer identifier
- logged-in user
- report date
- CPU model
- PassMark CPU Mark
- total installed memory
- total drive capacity
- worst drive health status
- link from the computer name to a generated detail page

If multiple report JSON files exist for the same computer, `hwoverview` shows only the most recent version in the main overview table.

Each generated detail page includes:

- a link to the source JSON
- previous/next navigation that loops across all versions of the same computer
- a version list so you can jump between snapshots easily

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
.\build.cmd
```

Or run the PowerShell script directly:

```powershell
pwsh -File .\build.ps1
```

The script builds both `hwreport.exe` and `hwoverview.exe` in the repository root and embeds:

- the semantic version derived from `VERSION` plus git commit height
- the current commit hash
- Windows version resource metadata for the executable properties dialog

On Windows, `build.cmd` is the preferred entrypoint. It bypasses local PowerShell execution policy restrictions for this script invocation and adds the default Go install location (`C:\Program Files\Go\bin`) to `PATH` when needed.

The Windows version-resource generator is tracked as a Go tool dependency in [go.mod](/abs/path/C:/Source/hwreport/go.mod:1). On a clean checkout, the first build may download that tool automatically through the normal Go module system. No separate manual install step or committed `.exe` helper is required.

## Testing

Run the normal unit test suite:

```powershell
go test ./...
```

The hard-drive benchmark lookup also has opt-in live integration tests under `internal/passmark`. These hit the real lookup site and are excluded from the default test run.

The integration test uses a hardcoded regression set of known drive models taken from the existing report corpus and verifies that the live lookup resolves benchmark data for each of them.

Run them explicitly when changing that lookup logic:

```powershell
go test -tags integration ./internal/passmark
```

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
- `hwoverview` uses PassMark's CPU lookup pages to resolve CPU Mark values and caches those lookups locally in the input directory.
