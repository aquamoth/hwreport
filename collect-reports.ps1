#Requires -Version 3.0
<#
.SYNOPSIS
Runs hwreport once on each reachable domain computer, in the signed-in user's
session, and collects the JSON reports into one local folder.

.DESCRIPTION
Run this from a domain-joined office PC or server as an account that is a
local administrator on the targets. For each computer it:

  1. connects over WinRM (all computers in parallel),
  2. copies hwreportw.exe into a temporary folder under %ProgramData%,
  3. finds the signed-in user and registers a temporary scheduled task for
     exactly that user, with no trigger,
  4. starts that task once, waits for it to finish, and reads the report,
  5. deletes the task and the temporary folder again.

So hwreport starts at most once per computer per run, as the real user (it
sees that user's monitors, resolution and rotation), and nothing stays behind
on the computer. Computers where nobody is signed in are skipped and listed,
unless you pass -AllowNoUser.

Computers that already have a report in -OutputDir are skipped as well, so
running the script again only reaches the ones that are still missing. Pass
-IncludeReported to collect from them again.

Finally it writes a CSV with each host's result and builds overview.html if
hwoverview.exe sits next to the exe.

Works in Windows PowerShell 3.0 and later and PowerShell 7. Requirements:
  - WinRM enabled on the targets (see README.md, GPO "WinRM - LAN admins")
  - the RSAT ActiveDirectory module, unless you pass -ComputerName

.PARAMETER OutputDir
Local folder that receives the JSON reports. Created if missing.

.PARAMETER ExePath
Path to the exe to run. Defaults to hwreportw.exe (no console window) next
to this script.

.PARAMETER Credential
Admin account to connect with. Omit it to use the logged-in account.

.PARAMETER ComputerName
Explicit list of computers. Skips the Active Directory lookup.

.PARAMETER Filter
Extra Get-ADComputer filter clause, for example "Name -like 'OFFICE-*'".

.PARAMETER SearchBase
AD distinguished name to search under, for example
"OU=Workstations,DC=CosmoJPN,DC=local".

.PARAMETER AllowNoUser
On computers where nobody is signed in, run hwreport in the WinRM session
instead of skipping. Monitor resolution and rotation are then missing.

.PARAMETER IncludeReported
Also run on computers that already have a report in -OutputDir.

.PARAMETER Authentication
WinRM authentication mechanism. The default works from a domain-joined
machine. From a machine outside the domain, pass Kerberos together with
-Credential and fully qualified computer names.

.PARAMETER ThrottleLimit
Maximum number of hosts handled at the same time.

.EXAMPLE
.\collect-reports.ps1 -SearchBase "OU=Workstations,DC=CosmoJPN,DC=local" -OutputDir E:\Shares\hwreport\reports

.EXAMPLE
.\collect-reports.ps1 -ComputerName PC-014, PC-022 -OutputDir E:\Shares\hwreport\reports
#>
[CmdletBinding()]
param(
    [string]   $OutputDir = '.\reports',
    [string]   $ExePath,
    [System.Management.Automation.PSCredential][System.Management.Automation.Credential()]
               $Credential = [System.Management.Automation.PSCredential]::Empty,
    [string[]] $ComputerName,
    [string]   $Filter,
    [string]   $SearchBase,
    [switch]   $AllowNoUser,
    [switch]   $IncludeReported,
    [System.Management.Automation.Runspaces.AuthenticationMechanism]
               $Authentication = 'Default',
    [int]      $ThrottleLimit = 32
)

$ErrorActionPreference = 'Stop'

if (-not $ExePath) { $ExePath = Join-Path $PSScriptRoot 'hwreportw.exe' }
# ProviderPath, not Path: for a UNC folder .Path carries a PowerShell provider
# prefix that .NET file APIs reject.
$ExePath = (Resolve-Path -LiteralPath $ExePath).ProviderPath
$null = New-Item -ItemType Directory -Force -Path $OutputDir
$OutputDir = (Resolve-Path -LiteralPath $OutputDir).ProviderPath

$credSplat = @{}
if ($Credential -ne [System.Management.Automation.PSCredential]::Empty) { $credSplat.Credential = $Credential }

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

$results = @{}
foreach ($t in $targets) {
    $results[$t] = [pscustomobject]@{ Host = $t; Status = 'FAIL'; Context = ''; Report = ''; Detail = '' }
}

if (-not $IncludeReported) {
    # Hostnames that already have a report in the output folder.
    $reported = @{}
    foreach ($f in Get-ChildItem -LiteralPath $OutputDir -Filter '*.json' -File -ErrorAction SilentlyContinue) {
        if ($f.Name -like '.*') { continue }
        try { $h = (Get-Content -LiteralPath $f.FullName -Raw | ConvertFrom-Json).hostname } catch { $h = $null }
        if ($h) { $reported[$h.ToUpper()] = $f.Name }
    }
    $remaining = @()
    foreach ($t in $targets) {
        $short = $t.Split('.')[0].ToUpper()
        if ($reported.ContainsKey($short)) {
            $results[$t].Status = 'SKIPPED'
            $results[$t].Report = $reported[$short]
            $results[$t].Detail = 'Already reported (use -IncludeReported to run again).'
        } else {
            $remaining += $t
        }
    }
    $targets = $remaining
}

Write-Host ("{0} computers to run on. Connecting over WinRM..." -f $targets.Count)

# --- 2. Open sessions (in parallel) -----------------------------------------

$sessions = @()
if ($targets.Count -gt 0) {
    $sessionOption = New-PSSessionOption -OpenTimeout 15000
    $connectErrors = @()
    $sessions = @(New-PSSession -ComputerName $targets -SessionOption $sessionOption `
            -Authentication $Authentication -ThrottleLimit $ThrottleLimit -ErrorAction SilentlyContinue `
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
    Write-Host ("Connected to {0} of {1}. Copying {2}..." -f $sessions.Count, $targets.Count, (Split-Path -Leaf $ExePath))
}

$runId = [guid]::NewGuid().ToString('N')
$exeName = Split-Path -Leaf $ExePath
$allowNoUserFlag = $AllowNoUser.IsPresent

try {
    # --- 3. Copy the exe to all sessions -------------------------------------
    # Copy-Item -ToSession needs PowerShell 5, so send the exe in base64 chunks
    # to every session at once. Each chunk stays far below the 10 MB remoting
    # object limit. The folder is %ProgramData%\hwreport-run-<id>, which the
    # signed-in user can read and execute from.

    $bytes = [IO.File]::ReadAllBytes($ExePath)
    $chunkSize = 1MB
    $ready = @($sessions)

    for ($offset = 0; $offset -lt $bytes.Length -and $ready.Count -gt 0; $offset += $chunkSize) {
        $length = [Math]::Min($chunkSize, $bytes.Length - $offset)
        $data = [Convert]::ToBase64String($bytes, $offset, $length)
        $copyErrors = @()
        Invoke-Command -Session $ready -ThrottleLimit $ThrottleLimit `
            -ArgumentList $runId, $exeName, $data, ($offset -eq 0) `
            -ErrorAction SilentlyContinue -ErrorVariable copyErrors -ScriptBlock {
            param([string]$RunId, [string]$Name, [string]$Data, [bool]$First)
            $dir = Join-Path $env:ProgramData "hwreport-run-$RunId"
            if ($First) { $null = New-Item -ItemType Directory -Force -Path $dir; $mode = [IO.FileMode]::Create }
            else { $mode = [IO.FileMode]::Append }
            $buffer = [Convert]::FromBase64String($Data)
            $stream = New-Object IO.FileStream (Join-Path $dir $Name), $mode
            try { $stream.Write($buffer, 0, $buffer.Length) } finally { $stream.Close() }
        }
        foreach ($e in $copyErrors) {
            $origin = $e.OriginInfo.PSComputerName
            if ($origin -and $results.ContainsKey($origin)) {
                $results[$origin].Detail = "Copy: $($e.Exception.Message)"
                $ready = @($ready | Where-Object { $_.ComputerName -ne $origin })
            }
        }
    }

    # --- 4. Run hwreport once per computer -----------------------------------

    $runErrors = @()
    $outputs = @()
    if ($ready.Count -gt 0) {
        Write-Host ("Running hwreport on {0} computers..." -f $ready.Count)
        $outputs = @(Invoke-Command -Session $ready -ThrottleLimit $ThrottleLimit `
            -ArgumentList $runId, $exeName, $allowNoUserFlag `
            -ErrorAction SilentlyContinue -ErrorVariable runErrors -ScriptBlock {
            param([string]$RunId, [string]$Name, [bool]$AllowNoUser)
            $ErrorActionPreference = 'Stop'
            $dir = Join-Path $env:ProgramData "hwreport-run-$RunId"
            $exe = Join-Path $dir $Name
            $out = Join-Path $dir 'report.json'
            $exeArgs = '--out "{0}"' -f $out

            # The console user, or else the owner of a running explorer.exe (RDP).
            $user = (Get-CimInstance Win32_ComputerSystem).UserName
            if (-not $user) {
                foreach ($p in @(Get-CimInstance Win32_Process -Filter "Name='explorer.exe'")) {
                    $o = Invoke-CimMethod -InputObject $p -MethodName GetOwner
                    if ($o.ReturnValue -eq 0 -and $o.User) {
                        if ($o.Domain) { $user = "{0}\{1}" -f $o.Domain, $o.User } else { $user = $o.User }
                        break
                    }
                }
            }

            if ($user) {
                $acl = Get-Acl -LiteralPath $dir
                $acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule (
                    $user, 'Modify', 'ContainerInherit,ObjectInherit', 'None', 'Allow')))
                Set-Acl -LiteralPath $dir -AclObject $acl

                # A task with no trigger: it runs only when started below, once.
                $taskName = "hwreport-$RunId"
                $action    = New-ScheduledTaskAction -Execute $exe -Argument $exeArgs
                $principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
                $settings  = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 10) `
                    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -MultipleInstances IgnoreNew
                Register-ScheduledTask -TaskName $taskName -Action $action -Principal $principal -Settings $settings | Out-Null
                try {
                    Start-ScheduledTask -TaskName $taskName
                    $startBy = (Get-Date).AddSeconds(60)
                    $finishBy = (Get-Date).AddMinutes(10)
                    while ($true) {
                        Start-Sleep -Seconds 2
                        $state = (Get-ScheduledTask -TaskName $taskName).State
                        $result = (Get-ScheduledTaskInfo -TaskName $taskName).LastTaskResult
                        $notRunYet = ($result -eq 267011)   # SCHED_S_TASK_HAS_NOT_RUN
                        if ("$state" -eq 'Ready' -and -not $notRunYet) { break }
                        if ($notRunYet -and "$state" -ne 'Running' -and (Get-Date) -gt $startBy) {
                            throw "The task did not start in $user's session."
                        }
                        if ((Get-Date) -gt $finishBy) {
                            Stop-ScheduledTask -TaskName $taskName
                            throw 'hwreport did not finish within 10 minutes and was stopped.'
                        }
                    }
                    if ($result -ne 0) { throw ("hwreport exited with code {0} in {1}'s session." -f $result, $user) }
                } finally {
                    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
                }
                $context = "user: $user"
            } elseif ($AllowNoUser) {
                $p = Start-Process -FilePath $exe -ArgumentList $exeArgs -Wait -PassThru -WindowStyle Hidden
                if ($p.ExitCode -ne 0) { throw "hwreport exited with code $($p.ExitCode)." }
                $context = 'nobody signed in (WinRM session)'
            } else {
                return New-Object PSObject -Property @{ ComputerName = $env:COMPUTERNAME; Json = $null; Context = 'nobody signed in' }
            }

            if (-not (Test-Path -LiteralPath $out)) { throw 'hwreport finished but wrote no report.' }
            New-Object PSObject -Property @{
                ComputerName = $env:COMPUTERNAME
                Json         = [IO.File]::ReadAllText($out)
                Context      = $context
            }
        })
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
        $r = $results[$o.PSComputerName]
        $r.Context = $o.Context
        if (-not $o.Json) {
            $r.Status = 'SKIPPED'
            $r.Detail = 'Nobody signed in; hwreport was not started.'
            continue
        }
        $base = "{0}-{1}" -f $o.ComputerName, $date
        $path = Join-Path $OutputDir "$base.json"
        $i = 1
        while (Test-Path -LiteralPath $path) {
            $path = Join-Path $OutputDir ("{0}-{1}.json" -f $base, $i)
            $i++
        }
        try {
            [IO.File]::WriteAllText($path, $o.Json, $utf8)
            $r.Status = 'OK'
            $r.Detail = ''
            $r.Report = Split-Path -Leaf $path
        } catch {
            # Never lose a collected report: fall back to the local temp folder.
            $fallback = Join-Path ([IO.Path]::GetTempPath()) "$base-$runId.json"
            [IO.File]::WriteAllText($fallback, $o.Json, $utf8)
            $r.Status = 'OK'
            $r.Report = $fallback
            $r.Detail = "Could not write to $OutputDir ($($_.Exception.Message)); saved locally instead."
        }
    }
} finally {
    if ($sessions.Count -gt 0) {
        # Remove the temporary folder everywhere, including hosts that failed.
        Invoke-Command -Session $sessions -ThrottleLimit $ThrottleLimit -ArgumentList $runId `
            -ErrorAction SilentlyContinue -ScriptBlock {
            param([string]$RunId)
            Remove-Item -LiteralPath (Join-Path $env:ProgramData "hwreport-run-$RunId") -Recurse -Force -ErrorAction SilentlyContinue
        }
        Remove-PSSession -Session $sessions
    }
}

foreach ($r in $results.Values) {
    if ($r.Status -eq 'FAIL' -and -not $r.Detail) { $r.Detail = 'No output returned.' }
}

# --- 6. Summary, CSV and overview -------------------------------------------

$rows = @($results.Values | Sort-Object Status, Host)
$csv = Join-Path $OutputDir ("collect-{0}.csv" -f (Get-Date -Format 'yyyy-MM-dd-HHmm'))
$csvArgs = @{ LiteralPath = $csv; Encoding = 'UTF8' }
if ($PSVersionTable.PSVersion.Major -lt 6) { $csvArgs.NoTypeInformation = $true }
$rows | Export-Csv @csvArgs
$rows | Format-Table Host, Status, Context, Report, Detail -AutoSize -Wrap

$ok      = @($rows | Where-Object Status -eq 'OK').Count
$skipped = @($rows | Where-Object Status -eq 'SKIPPED').Count
Write-Host ("Done. {0} collected, {1} skipped, {2} failed. Reports in {3}" -f $ok, $skipped, ($rows.Count - $ok - $skipped), $OutputDir)
Write-Host ("Per-host results: {0}" -f $csv)

$overviewExe = Join-Path (Split-Path -Parent $ExePath) 'hwoverview.exe'
if ($ok -gt 0 -and (Test-Path -LiteralPath $overviewExe)) {
    $overviewHtml = Join-Path $OutputDir 'overview.html'
    Write-Host 'Building overview...'
    & $overviewExe --in $OutputDir --out $overviewHtml
    if ($LASTEXITCODE -eq 0) { Write-Host ("Overview: {0}" -f $overviewHtml) }
}
