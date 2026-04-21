<#
.SYNOPSIS
Copies hwreport.exe and the USB launcher files to the root of the given
removable drive.

.EXAMPLE
.\deploy-usb.ps1 -Drive E:
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Drive
)

$ErrorActionPreference = 'Stop'

$target = ($Drive.TrimEnd('\').TrimEnd(':')) + ':\'
if (-not (Test-Path -LiteralPath $target)) {
    throw "Drive $target not found. Is the stick plugged in?"
}

$repo = $PSScriptRoot
$files = @(
    (Join-Path $repo 'hwreport.exe'),
    (Join-Path $repo 'usb-stick\autorun.inf'),
    (Join-Path $repo 'usb-stick\run-report.ps1'),
    (Join-Path $repo 'usb-stick\START HERE.cmd')
)

foreach ($f in $files) {
    if (-not (Test-Path -LiteralPath $f)) { throw "Missing source file: $f" }
}

Write-Host "Deploying to $target"
foreach ($f in $files) {
    Copy-Item -LiteralPath $f -Destination $target -Force
    Write-Host ("  + {0}" -f (Split-Path -Leaf $f))
}

Write-Host ""
Write-Host "Done. Safely eject the stick before removing it." -ForegroundColor Green
