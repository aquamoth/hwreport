# hwreport

`hwreport` is a small toolkit for collecting and summarizing Windows hardware inventory data.

It currently contains two standalone Windows executables:

- `hwreport.exe` collects a JSON report from a single computer (`hwreportw.exe` is the same program without a console window, for scheduled tasks)
- `hwoverview.exe` aggregates many report JSON files into an HTML overview table

The binary is written in Go. The executable can be copied to removable media and run on Windows 11 computers to produce per-machine report files for later aggregation.

## hwreport: What It Collects

- Computer manufacturer and model
- Computer first-use date: the earliest Windows install date, which survives in-place upgrades (`HKLM\SYSTEM\Setup\Source OS`)
- Currently logged-in console user, when one is present
- OS name, version, and first install date
- CPU manufacturer and model
- Memory manufacturer, model, type, installed capacity, free memory, populated slots, and empty slot count
- Storage devices, size, type, bus type (NVMe, SATA, ...), best-effort SMART status, and each drive letter's size and free space
- GPU manufacturer and model
- Active monitors, pixel and physical size when available, and current rotation
- EDID-derived monitor identifiers and physical size when Windows exposes monitor EDID data

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
- status: OK, Notice, Warning or Critical, with the number of findings (hover for the reasons); the first click sorts worst first
- link from the computer name to a generated detail page

The status findings, with thresholds kept together in [internal/overview/assessment.go](internal/overview/assessment.go):

| Level | Finding |
| --- | --- |
| Critical | a drive reports a SMART warning or error; C: has under 10 GB or 5% free |
| Warning | C: has under 30 GB or 10% free; Windows runs from a hard disk; under 8 GB memory; CPU Mark under 6,000; CPU on the market 8+ years |
| Notice | a hard disk for data; under 16 GB memory; CPU Mark under 10,000; CPU on the market 6+ years |

If multiple report JSON files exist for the same computer, `hwoverview` shows only the most recent version in the main overview table.

Each generated detail page includes:

- the status findings
- each drive's tier (NVMe SSD, SATA SSD or HDD, from the bus type; marked "est." when estimated from the Drive Mark for reports without one), a speed class, and the free space per drive letter
- when the CPU came on the market (PassMark's "first seen" date) and the first and current Windows install dates
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

## Collecting From Every Office Computer

`collect-reports.ps1` runs on a server as an admin account and reaches every workstation over PowerShell remoting (WinRM). It runs `hwreportw.exe` once in the signed-in user's session, so the report includes that user's monitors, resolution and rotation, and saves the report in one folder. Run it hourly from a scheduled task and it keeps going until every PC has reported.

### One-time setup

[deploy/Install-HwreportGpos.ps1](deploy/Install-HwreportGpos.ps1) runs on a domain controller as a Domain Admin. It creates:

- **Share `\\<dc>\hwreport$`** (default `E:\Shares\hwreport`, reports in `reports\`), for administrators only.
- **OU `Workstations`** at the domain root. GPOs can't be linked to the default `Computers` container, so move the workstations here.
- **GPO `WinRM - LAN admins`**: enables WinRM. The firewall allows TCP 5985 only from the office subnet and only on the domain profile, and the WinRM service is set to automatic and started. Over the network Windows filters local accounts out of the Administrators token, so only domain accounts that are local admins get in.

The script links nothing. Link the GPO to the Workstations OU, then copy `hwreportw.exe`, `hwoverview.exe` and `collect-reports.ps1` to the share.

`hwreportw.exe` is `hwreport.exe` built as a Windows GUI-subsystem program, so it opens no console window in the user's session. It runs as the normal user: disk health, free space, monitors, resolution and rotation all work without admin rights. It stops itself if collecting takes longer than 3 minutes.

### Enabling WinRM by hand

Without the GPO above, Windows 11 clients have WinRM turned off. The equivalent settings in a Group Policy Object are:

1. **Computer Configuration → Policies → Administrative Templates → Windows Components → Windows Remote Management (WinRM) → WinRM Service → Allow remote server management through WinRM**: Enabled, IPv4 filter `*`.
2. **Computer Configuration → Policies → Windows Settings → Security Settings → System Services → Windows Remote Management (WS-Management)**: Automatic.
3. **Computer Configuration → Policies → Windows Settings → Security Settings → Windows Defender Firewall with Advanced Security → Inbound Rules**: allow TCP 5985 from your office subnet, Domain profile.

PCs pick this up at their next policy refresh (or run `gpupdate /force` on them, or reboot). The account you collect with must be a local administrator on the PCs, which Domain Admins are by default.

### Running a collection

Run from a domain-joined server or PC, with `hwreportw.exe` (and optionally `hwoverview.exe`) next to the script. Getting the computer list from AD needs the RSAT ActiveDirectory module. Windows PowerShell 3.0 or later (Server 2012 R2 has 4.0) and PowerShell 7 all work.

```powershell
# All workstations, as the logged-in admin account
.\collect-reports.ps1 -SearchBase "OU=Workstations,DC=office,DC=local" -OutputDir E:\Shares\hwreport\reports

# A few machines, as an admin account you are prompted for
.\collect-reports.ps1 -ComputerName PC-014, PC-022 -Credential (Get-Credential OFFICE\admin) -OutputDir E:\Shares\hwreport\reports
```

For each reachable PC the script copies `hwreportw.exe` to a temporary folder under `%ProgramData%`, registers a temporary scheduled task for the signed-in user, starts it once, waits for it, saves the report as `HOSTNAME-YYYY-MM-DD.json`, and deletes the task and the folder again. Nothing stays behind on the PC. If nobody is signed in, the PC is skipped (pass `-AllowNoUser` to run in the WinRM session instead, without monitor resolution and rotation). If saving the report fails, nothing is kept on the PC either; the next run tries again.

A PC counts as complete once the output folder holds a report for it that was taken in a user session by a version of hwreport that writes at least `-MinSchemaVersion` (default 4, the first with free space per drive). Complete PCs are skipped, so the script can run every hour and only reaches the PCs still missing. Raising `-MinSchemaVersion` after a new collector version starts a new round; `-IncludeReported` collects from every PC again.

Each run rewrites `progress.csv` with one row per PC (`COMPLETE`, `COLLECTED`, `COLLECTED-NO-USER`, `WAITING`, `UNREACHABLE` or `FAILED`), appends what happened to `collect-history.csv`, ends with a line like `29 of 60 complete`, and rebuilds `overview.html` when something new came in.

To run it hourly on weekdays, create a scheduled task on the server that runs as the admin account (stored password, highest privileges):

```text
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& { Start-Transcript -Path 'E:\Shares\hwreport\reports\collect-transcript.log' -Append | Out-Null; try { & 'E:\Shares\hwreport\collect-reports.ps1' -SearchBase 'OU=Workstations,DC=office,DC=local' -OutputDir 'E:\Shares\hwreport\reports' } finally { Stop-Transcript | Out-Null } }"
```

Disable the task once `progress.csv` shows every PC as complete.

## Building

Requirements:

- Go 1.26 or newer
- Git, to compute the build version and commit metadata

Build the latest checked-out version with the provided script:

```powershell
.\build.cmd
```

Or run the Go-native build command directly:

```powershell
go run ./cmd/buildtool
```

The build command produces `hwreport.exe`, `hwreportw.exe` and `hwoverview.exe` in the repository root and embeds:

- the semantic version derived from `VERSION` plus git commit height
- the current commit hash
- Windows version resource metadata for the executable properties dialog

On Windows, `build.cmd` is just a thin convenience wrapper around `go run ./cmd/buildtool`.

The Windows version-resource generator is tracked as a Go tool dependency in [go.mod](/abs/path/C:/Source/hwreport/go.mod:1). The build command invokes it through `go tool`, so on a clean checkout Go may download that pinned tool automatically through the normal module system. There is no custom downloader in the repo and no committed helper `.exe`.

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

`patch` is calculated by the build command as the number of commits since the last commit that changed `VERSION`, inclusive of that change commit. In practice:

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
- `hwoverview` uses PassMark's CPU lookup pages to resolve CPU Mark values and first-seen dates, and caches those lookups locally in the input directory.
- Drive benchmarks come from harddrivebenchmark.net's full drive list, downloaded once a month into the input directory (`.hwoverview-drive-list.json`) and matched locally by model code and capacity.
- `hwreport` stays offline-only. Monitor physical size is taken from local EDID parsing when possible, with WMI used only as a fallback.
- `hwoverview` may enrich missing or weak monitor physical-size data using model-level monitor IDs such as `BOE0CB4`, and caches those lookups locally in the input directory.
