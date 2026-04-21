#Requires -Modules ActiveDirectory
<#
.SYNOPSIS
Collects hwreport JSON files from every reachable domain-joined Windows
computer and stores them in a local folder.

.DESCRIPTION
Enumerates enabled Windows computers from Active Directory, probes WinRM
(TCP 5985) in parallel to decide which are online, then for each online
host opens a PSSession, copies hwreport.exe to the remote TEMP, runs it,
copies the resulting JSON back, and cleans up.

The caller must be able to:
  - query AD (RSAT ActiveDirectory module installed)
  - open WinRM sessions to the targets as an administrator

.PARAMETER OutputDir
Local folder that will receive the JSON reports. Created if missing.

.PARAMETER ExePath
Path to the hwreport.exe to deploy. Defaults to .\hwreport.exe.

.PARAMETER ThrottleLimit
Maximum number of hosts processed in parallel.

.PARAMETER Filter
Optional additional AD filter clause (LDAP-style PowerShell filter). For
example: "Name -like 'OFFICE-*'" to restrict to a naming prefix.

.EXAMPLE
.\collect-reports.ps1 -OutputDir .\reports

.EXAMPLE
.\collect-reports.ps1 -OutputDir D:\hwreport -Filter "Name -like 'LAB-*'"
#>
[CmdletBinding()]
param(
    [string]$OutputDir     = ".\reports",
    [string]$ExePath       = ".\hwreport.exe",
    [int]   $ThrottleLimit = 16,
    [string]$Filter
)

$ErrorActionPreference = 'Stop'

$ExePath = (Resolve-Path -LiteralPath $ExePath).Path
$null    = New-Item -ItemType Directory -Force -Path $OutputDir
$OutputDir = (Resolve-Path -LiteralPath $OutputDir).Path

Write-Host "Querying Active Directory..."
$adFilter = "Enabled -eq `$true -and OperatingSystem -like '*Windows*'"
if ($Filter) { $adFilter = "($adFilter) -and ($Filter)" }

$computers = Get-ADComputer -Filter $adFilter -Properties DNSHostName |
    Where-Object DNSHostName |
    Select-Object -ExpandProperty DNSHostName

Write-Host ("AD returned {0} enabled Windows computers. Probing WinRM (5985)..." -f $computers.Count)

$reachable = $computers | ForEach-Object -ThrottleLimit $ThrottleLimit -Parallel {
    try {
        $c = New-Object System.Net.Sockets.TcpClient
        $iar = $c.BeginConnect($_, 5985, $null, $null)
        if ($iar.AsyncWaitHandle.WaitOne(1500, $false) -and $c.Connected) {
            $c.EndConnect($iar) | Out-Null
            $_
        }
        $c.Close()
    } catch { }
}

Write-Host ("{0} reachable over WinRM. Collecting reports..." -f @($reachable).Count)

$results = $reachable | ForEach-Object -ThrottleLimit $ThrottleLimit -Parallel {
    $name   = $_
    $exe    = $using:ExePath
    $dir    = $using:OutputDir
    $short  = $name.Split('.')[0]

    try {
        $session = New-PSSession -ComputerName $name -ErrorAction Stop
        try {
            $remoteExe = Invoke-Command -Session $session -ScriptBlock {
                Join-Path $env:TEMP 'hwreport.exe'
            }
            $remoteOut = Invoke-Command -Session $session -ScriptBlock {
                Join-Path $env:TEMP ("hwreport-{0}.json" -f [guid]::NewGuid().ToString('N'))
            }

            Copy-Item -LiteralPath $exe -Destination $remoteExe -ToSession $session -Force

            Invoke-Command -Session $session -ScriptBlock {
                param($e, $o)
                & $e --out $o | Out-Null
                if ($LASTEXITCODE -ne 0) { throw "hwreport exited $LASTEXITCODE" }
            } -ArgumentList $remoteExe, $remoteOut

            $localName = "{0}-{1}.json" -f $short, (Get-Date -Format 'yyyy-MM-dd')
            $localPath = Join-Path $dir $localName
            $i = 1
            while (Test-Path -LiteralPath $localPath) {
                $localPath = Join-Path $dir ("{0}-{1}-{2}.json" -f $short, (Get-Date -Format 'yyyy-MM-dd'), $i)
                $i++
            }
            Copy-Item -LiteralPath $remoteOut -Destination $localPath -FromSession $session -Force

            Invoke-Command -Session $session -ScriptBlock {
                param($e, $o)
                Remove-Item -LiteralPath $e, $o -Force -ErrorAction SilentlyContinue
            } -ArgumentList $remoteExe, $remoteOut

            [pscustomobject]@{ Host = $name; Status = 'OK'; Path = $localPath }
        }
        finally { Remove-PSSession $session }
    }
    catch {
        [pscustomobject]@{ Host = $name; Status = 'FAIL'; Path = $_.Exception.Message }
    }
}

$ok   = @($results | Where-Object Status -eq 'OK')
$fail = @($results | Where-Object Status -eq 'FAIL')

$results | Sort-Object Status, Host | Format-Table -AutoSize

Write-Host ""
Write-Host ("Done. {0} succeeded, {1} failed. Reports in {2}" -f $ok.Count, $fail.Count, $OutputDir)
