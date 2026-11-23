# hwreport

`hwreport` is a small toolkit for collecting and summarizing Windows hardware inventory data.

It currently contains two standalone Windows executables:

- `hwreport.exe` collects a JSON report from a single computer (`hwreportw.exe` is the same program without a console window, for scheduled tasks)
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

## Collecting From Every Office Computer

There are two ways to collect from a whole domain, and they work together:

- **Group Policy (one report per PC, no remote access needed):** a scheduled task runs `hwreportw.exe` as the signed-in user and drops the report on a share. PCs that are off today report the next time someone signs in.
- **`collect-reports.ps1` (on demand):** runs `hwreport.exe` on every reachable PC over PowerShell remoting (WinRM) right now.

### Group Policy deployment

[deploy/Install-HwreportGpos.ps1](deploy/Install-HwreportGpos.ps1) runs on a domain controller as a Domain Admin. It creates:

- **Share `\\<dc>\hwreport$`** (default `E:\Shares\hwreport`). Domain users can run the exes from the root. In `reports\` they can create files but can't list or read them.
- **OU `Workstations`** at the domain root. GPOs can't be linked to the default `Computers` container, so move the workstations here.
- **GPO `WinRM - LAN admins`**: enables WinRM. The firewall allows TCP 5985 only from the office subnet and only on the domain profile, and the WinRM service is set to automatic and started. Over the network Windows filters local accounts out of the Administrators token, so only domain accounts that are local admins get in.
- **GPO `hwreport inventory`**: a scheduled task that runs `hwreportw.exe --out <share>\reports --once <key>` as `BUILTIN\Users` at least privilege, in each signed-in user's session. It fires 5 minutes after logon and daily at 11:00. Because of `--once`, each PC reports once per key. The task is set to "remove when no longer applied", so unlinking the GPO deletes it.

The script links nothing. Link each GPO where it should apply, then copy `hwreportw.exe`, `hwreport.exe`, `hwoverview.exe` and `collect-reports.ps1` to the share. To run a new inventory round later, change the `--once` key in the task (for example `inventory-2027-05`).

`hwreportw.exe` is `hwreport.exe` built as a Windows GUI-subsystem program, so a scheduled run opens no console window. It runs as a normal user: disk health, monitors, resolution and rotation all work without admin rights.

### Enabling WinRM by hand

Without the GPO above, Windows 11 clients have WinRM turned off. The equivalent settings in a Group Policy Object are:

1. **Computer Configuration → Policies → Administrative Templates → Windows Components → Windows Remote Management (WinRM) → WinRM Service → Allow remote server management through WinRM**: Enabled, IPv4 filter `*`.
2. **Computer Configuration → Policies → Windows Settings → Security Settings → System Services → Windows Remote Management (WS-Management)**: Automatic.
3. **Computer Configuration → Policies → Windows Settings → Security Settings → Windows Defender Firewall with Advanced Security → Inbound Rules**: allow TCP 5985 from your office subnet, Domain profile.

PCs pick this up at their next policy refresh (or run `gpupdate /force` on them, or reboot). The account you collect with must be a local administrator on the PCs, which Domain Admins are by default.

### Running a collection over WinRM

Run from a domain-joined office PC or server, with `hwreport.exe` (and optionally `hwoverview.exe`) next to the script. Getting the computer list from AD needs the RSAT ActiveDirectory module. Windows PowerShell 3.0 or later (Server 2012 R2 has 4.0) and PowerShell 7 all work.

```powershell
# Every enabled Windows computer in AD, as an admin account you are prompted for
.\collect-reports.ps1 -Credential (Get-Credential OFFICE\admin) -OutputDir D:\hwreport

# Only one OU, as the logged-in account
.\collect-reports.ps1 -SearchBase "OU=Workstations,DC=office,DC=local" -OutputDir D:\hwreport

# Retry the machines that were off last time
.\collect-reports.ps1 -ComputerName PC-014, PC-022 -OutputDir D:\hwreport
```

The script opens sessions to all machines in parallel, copies `hwreport.exe` into the remote temp folder, runs it, pulls the JSON back and deletes both files from the remote machine. The reports are named `HOSTNAME-YYYY-MM-DD.json`, the same as a local run. Each run also writes `collect-YYYY-MM-DD-HHmm.csv` with every host's result and the reason it failed, if it did. If `hwoverview.exe` is present, the script finishes by building `overview.html` in the output folder.

Machines that are off or asleep show up as failed. Run the script again later. Old reports are kept, and the overview shows the newest one per computer.

Limitation: a remote run has no desktop session, so expect monitor **rotation** and **current resolution** to be empty. Monitor make, model and physical size come from EDID through WMI and are still reported.

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

The build command produces both `hwreport.exe` and `hwoverview.exe` in the repository root and embeds:

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
- `hwoverview` uses PassMark's CPU lookup pages to resolve CPU Mark values and caches those lookups locally in the input directory.
- `hwreport` stays offline-only. Monitor physical size is taken from local EDID parsing when possible, with WMI used only as a fallback.
- `hwoverview` may enrich missing or weak monitor physical-size data using model-level monitor IDs such as `BOE0CB4`, and caches those lookups locally in the input directory.
