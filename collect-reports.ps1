#Requires -Version 5.1
<#
.SYNOPSIS
Runs hwreport.exe on every reachable domain computer over WinRM and collects
the JSON reports into one local folder.

.DESCRIPTION
Run this from a domain-joined office PC or server. It:

  1. lists enabled Windows computers from Active Directory (or uses -ComputerName),
  2. opens a PowerShell remoting (WinRM) session to all of them in parallel,
  3. copies hwreport.exe to each machine, runs it, and pulls the JSON back,
  4. removes hwreport.exe and the JSON from the remote machine again,
  5. writes a CSV with each host's result next to the reports, and
  6. builds overview.html if hwoverview.exe sits next to hwreport.exe.

Works in Windows PowerShell 5.1 and PowerShell 7. Requirements:
  - WinRM enabled on the target PCs (Windows 11 clients have it off by
    default; enable it by Group Policy, see README.md)
  - an account that is a local administrator on the targets
  - the RSAT ActiveDirectory module, unless you pass -ComputerName

Machines that are off, asleep or unreachable are listed as failed in the CSV.
Run the script again later with -ComputerName set to just those.

.PARAMETER OutputDir
Local folder that receives the JSON reports. Created if missing.

.PARAMETER ExePath
Path to hwreport.exe. Defaults to hwreport.exe next to this script.

.PARAMETER Credential
Admin account to connect with. Omit it to use the logged-in account.
Pass (Get-Credential) to be prompted, for example for OFFICE\admin.

.PARAMETER ComputerName
Explicit list of computers. Skips the Active Directory lookup.

.PARAMETER Filter
Extra Get-ADComputer filter clause, for example "Name -like 'OFFICE-*'".

.PARAMETER SearchBase
AD distinguished name to search under, for example
"OU=Workstations,DC=office,DC=local".

.PARAMETER ThrottleLimit
Maximum number of hosts handled at the same time.

.EXAMPLE
.\collect-reports.ps1 -Credential (Get-Credential OFFICE\admin)

.EXAMPLE
.\collect-reports.ps1 -SearchBase "OU=Workstations,DC=office,DC=local" -OutputDir D:\hwreport

.EXAMPLE
.\collect-reports.ps1 -ComputerName PC-014, PC-022 -Credential (Get-Credential)
#>
[CmdletBinding()]
param(
    [string]   $OutputDir = '.\reports',
    [string]   $ExePath,
    [pscredential][System.Management.Automation.Credential()]
               $Credential = [pscredential]::Empty,
    [string[]] $ComputerName,
    [string]   $Filter,
    [string]   $SearchBase,
    [int]      $ThrottleLimit = 32
)

$ErrorActionPreference = 'Stop'

if (-not $ExePath) { $ExePath = Join-Path $PSScriptRoot 'hwreport.exe' }
$ExePath = (Resolve-Path -LiteralPath $ExePath).Path
$null = New-Item -ItemType Directory -Force -Path $OutputDir
$OutputDir = (Resolve-Path -LiteralPath $OutputDir).Path

$credSplat = @{}
if ($Credential -ne [pscredential]::Empty) { $credSplat.Credential = $Credential }

# --- 1. Target list ----------------------------------------------------------

if ($ComputerName) {
    # Also accept "PC-1,PC-2" as one string (what powershell.exe -File passes).
    $targets = @($ComputerName -split '[,\s]+' | Where-Object { $_ } | Sort-Object -Unique)
} else {
    if (-not (Get-Module -ListAvailable -Name ActiveDirectory)) {
        throw 'The ActiveDirectory module (RSAT) is not installed. Install it, or pass -ComputerName.'
    }
    Import-Module ActiveDirectory

    $adFilter = "Enabled -eq `$true -and OperatingSystem -like '*Windows*'"
    if ($Filter) { $adFilter = "($adFilter) -and ($Filter)" }
    $adArgs = @{ Filter = $adFilter; Properties = 'DNSHostName' } + $credSplat
    if ($SearchBase) { $adArgs.SearchBase = $SearchBase }

    Write-Host 'Querying Active Directory...'
    $targets = @(Get-ADComputer @adArgs |
        Where-Object DNSHostName |
        Select-Object -ExpandProperty DNSHostName |
        Sort-Object -Unique)
}

if ($targets.Count -eq 0) { throw 'No target computers found.' }
Write-Host ("{0} target computers. Connecting over WinRM..." -f $targets.Count)

# --- 2. Open sessions (in parallel) -----------------------------------------

$results = @{}
foreach ($t in $targets) {
    $results[$t] = [pscustomobject]@{ Host = $t; Status = 'FAIL'; Detail = ''; Report = '' }
}

$sessionOption = New-PSSessionOption -OpenTimeout 15000
$connectErrors = @()
$sessions = @(New-PSSession -ComputerName $targets -SessionOption $sessionOption `
        -ThrottleLimit $ThrottleLimit -ErrorAction SilentlyContinue `
        -ErrorVariable connectErrors @credSplat)

$connected = @{}
foreach ($s in $sessions) { $connected[$s.ComputerName] = $true }
foreach ($t in $targets) {
    if ($connected.ContainsKey($t)) { continue }
    $msg = $connectErrors |
        ForEach-Object { $_.Exception.Message } |
        Where-Object { $_ -like "*$t*" } |
        Select-Object -First 1
    if (-not $msg) { $msg = 'Could not connect.' }
    $results[$t].Detail = "Connect: $msg"
}

Write-Host ("Connected to {0} of {1}. Copying hwreport.exe..." -f $sessions.Count, $targets.Count)

try {
    # --- 3. Copy the exe to each session -------------------------------------

    $ready = @()
    foreach ($s in $sessions) {
        try {
            $remoteExe = Invoke-Command -Session $s -ScriptBlock {
                Join-Path $env:TEMP ("hwreport-{0}.exe" -f [guid]::NewGuid().ToString('N'))
            }
            Copy-Item -LiteralPath $ExePath -Destination $remoteExe -ToSession $s -Force
            $ready += [pscustomobject]@{ Session = $s; RemoteExe = $remoteExe }
        } catch {
            $results[$s.ComputerName].Detail = "Copy: $($_.Exception.Message)"
        }
    }

    # --- 4. Run hwreport on all of them at once ------------------------------

    Write-Host ("Running hwreport on {0} computers..." -f $ready.Count)

    $runErrors = @()
    $outputs = @()
    if ($ready.Count -gt 0) {
        # Each session gets its own exe path, so run per-session jobs in parallel.
        $jobs = foreach ($r in $ready) {
            Invoke-Command -Session $r.Session -AsJob -ArgumentList $r.RemoteExe -ScriptBlock {
                param([string]$ExeFile)
                $out = Join-Path $env:TEMP ("hwreport-{0}.json" -f [guid]::NewGuid().ToString('N'))
                try {
                    $text = & $ExeFile --out $out 2>&1 | ForEach-Object { "$_" }
                    if ($LASTEXITCODE -ne 0) {
                        throw ("hwreport exited with code {0}: {1}" -f $LASTEXITCODE, ($text -join ' '))
                    }
                    [pscustomobject]@{
                        ComputerName = $env:COMPUTERNAME
                        Json         = [IO.File]::ReadAllText($out)
                    }
                } finally {
                    Remove-Item -LiteralPath $ExeFile, $out -Force -ErrorAction SilentlyContinue
                }
            }
        }
        $outputs = @($jobs | Receive-Job -Wait -AutoRemoveJob -ErrorAction SilentlyContinue -ErrorVariable runErrors)
    }

    foreach ($e in $runErrors) {
        $origin = $e.OriginInfo.PSComputerName
        if ($origin -and $results.ContainsKey($origin)) {
            $results[$origin].Detail = "Run: $($e.Exception.Message)"
        }
    }

    # --- 5. Save the reports locally ----------------------------------------

    $utf8 = New-Object System.Text.UTF8Encoding $false
    $date = Get-Date -Format 'yyyy-MM-dd'
    foreach ($o in $outputs) {
        $hostKey = $o.PSComputerName
        $base = "{0}-{1}" -f $o.ComputerName, $date
        $path = Join-Path $OutputDir "$base.json"
        $i = 1
        while (Test-Path -LiteralPath $path) {
            $path = Join-Path $OutputDir ("{0}-{1}.json" -f $base, $i)
            $i++
        }
        [IO.File]::WriteAllText($path, $o.Json, $utf8)
        $results[$hostKey].Status = 'OK'
        $results[$hostKey].Detail = ''
        $results[$hostKey].Report = Split-Path -Leaf $path
    }
} finally {
    if ($sessions.Count -gt 0) { Remove-PSSession -Session $sessions }
}

foreach ($r in $results.Values) {
    if ($r.Status -ne 'OK' -and -not $r.Detail) { $r.Detail = 'No output returned.' }
}

# --- 6. Summary, CSV and overview -------------------------------------------

$rows = @($results.Values | Sort-Object Status, Host)
$csv = Join-Path $OutputDir ("collect-{0}.csv" -f (Get-Date -Format 'yyyy-MM-dd-HHmm'))
$csvArgs = @{ LiteralPath = $csv; Encoding = 'UTF8' }
if ($PSVersionTable.PSVersion.Major -lt 6) { $csvArgs.NoTypeInformation = $true }
$rows | Export-Csv @csvArgs
$rows | Format-Table Host, Status, Report, Detail -AutoSize -Wrap

$ok = @($rows | Where-Object Status -eq 'OK').Count
Write-Host ("Done. {0} succeeded, {1} failed. Reports in {2}" -f $ok, ($rows.Count - $ok), $OutputDir)
Write-Host ("Per-host results: {0}" -f $csv)

$overviewExe = Join-Path (Split-Path -Parent $ExePath) 'hwoverview.exe'
if ($ok -gt 0 -and (Test-Path -LiteralPath $overviewExe)) {
    $overviewHtml = Join-Path $OutputDir 'overview.html'
    Write-Host 'Building overview...'
    & $overviewExe --in $OutputDir --out $overviewHtml
    if ($LASTEXITCODE -eq 0) { Write-Host ("Overview: {0}" -f $overviewHtml) }
}
