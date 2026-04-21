<#
.SYNOPSIS
Runs hwreport.exe directly from the USB stick, writes the JSON to the
stick, then ejects it and beeps when it is safe to remove.

Invoked by "START HERE.cmd" via:
    Invoke-Expression (Get-Content -Raw -LiteralPath <this-file>)
so PowerShell never opens a persistent handle on it. Nothing is copied
to the host machine.
#>

# USB root is passed via an env var so this script works when loaded
# through Invoke-Expression (where $PSScriptRoot is empty).
$UsbRoot = $env:HWREPORT_USB
if (-not $UsbRoot) { throw 'HWREPORT_USB is not set. Launch via START HERE.cmd.' }

# Vacate the USB immediately: set BOTH PowerShell's location and the
# underlying .NET current directory, otherwise the process keeps an
# implicit handle on the USB root directory and Eject will refuse.
Set-Location -LiteralPath $env:SystemRoot
[Environment]::CurrentDirectory = $env:SystemRoot

$ErrorActionPreference = 'Stop'
$host.UI.RawUI.WindowTitle = 'Hardware Report'

Write-Host ''
Write-Host "Collecting hardware inventory on $env:COMPUTERNAME ..." -ForegroundColor Cyan
Write-Host ''

$outDir = Join-Path $UsbRoot 'reports'
if (-not (Test-Path -LiteralPath $outDir)) {
    New-Item -ItemType Directory -Path $outDir -Force | Out-Null
}

$today   = Get-Date -Format 'yyyy-MM-dd'
$outFile = Join-Path $outDir ("{0}-{1}.json" -f $env:COMPUTERNAME, $today)
$i = 1
while (Test-Path -LiteralPath $outFile) {
    $outFile = Join-Path $outDir ("{0}-{1}-{2}.json" -f $env:COMPUTERNAME, $today, $i)
    $i++
}

$hwexe = Join-Path $UsbRoot 'hwreport.exe'
$proc = Start-Process -FilePath $hwexe `
    -ArgumentList '--out', $outFile `
    -WorkingDirectory $env:SystemRoot `
    -Wait -PassThru -NoNewWindow
$exit = $proc.ExitCode

if ($exit -ne 0) {
    Write-Host ''
    Write-Host "hwreport failed with exit code $exit." -ForegroundColor Red
    Write-Host 'Press any key to close.'
    [void][System.Console]::ReadKey($true)
    exit $exit
}

Write-Host ''
Write-Host 'Report saved:' -ForegroundColor Green
Write-Host "  $outFile"
Write-Host ''

$usbDrive = ([System.IO.Path]::GetPathRoot($UsbRoot)).TrimEnd('\')

Write-Host "Ejecting $usbDrive ..."

# Give the system a beat to finish releasing the hwreport.exe image
# section handle before we ask the shell to eject the volume.
[System.GC]::Collect()
[System.GC]::WaitForPendingFinalizers()
Start-Sleep -Milliseconds 300

try {
    $shell = New-Object -ComObject Shell.Application
    $ns    = $shell.Namespace(17)   # 17 = "My Computer"
    $drive = $ns.ParseName($usbDrive)
    if ($drive) { $drive.InvokeVerb('Eject') }
    Start-Sleep -Milliseconds 1000
} catch {
    Write-Warning "Eject failed: $($_.Exception.Message)"
}

Start-Sleep -Milliseconds 500
$stillPresent = Test-Path -LiteralPath $UsbRoot

if ($stillPresent) {
    Write-Host ''
    Write-Host 'Drive did not eject cleanly. Use Safely Remove Hardware before unplugging.' -ForegroundColor Yellow
    [Console]::Beep(220, 400)
} else {
    [Console]::Beep(880, 150)
    [Console]::Beep(1320, 250)
    Write-Host ''
    Write-Host 'Safe to remove the USB stick.' -ForegroundColor Green
}

Write-Host ''
Write-Host 'This window will close in 5 seconds.'
Start-Sleep -Seconds 5
