$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $repoRoot

$hwreportOutput = Join-Path $repoRoot "hwreport.exe"
$hwoverviewOutput = Join-Path $repoRoot "hwoverview.exe"
$env:GOCACHE = Join-Path $repoRoot ".gocache"
$versionFile = Join-Path $repoRoot "VERSION"
$goversioninfo = Join-Path $repoRoot ".tools\goversioninfo.exe"
$hwreportSyso = Join-Path $repoRoot "cmd\hwreport\zz_versioninfo.syso"
$hwoverviewSyso = Join-Path $repoRoot "cmd\hwoverview\zz_versioninfo.syso"
$hwreportVersionJSON = Join-Path $repoRoot "cmd\hwreport\zz_versioninfo.json"
$hwoverviewVersionJSON = Join-Path $repoRoot "cmd\hwoverview\zz_versioninfo.json"

$versionPrefix = "0.0"
$version = "0.0.0"
$commit = "unknown"

if (-not (Test-Path $versionFile)) {
    throw "VERSION file not found: $versionFile"
}

$versionPrefix = (Get-Content $versionFile -Raw).Trim()
if ($versionPrefix -notmatch '^\d+\.\d+$') {
    throw "VERSION must contain major.minor, for example 1.0"
}

$patch = 0
$baselineCommit = $null

try {
    $baselineCommit = git log -n 1 --format=%H -- VERSION 2>$null
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($baselineCommit)) {
        $baselineCommit = $baselineCommit.Trim()
        $patchAhead = git rev-list --count "$baselineCommit..HEAD" 2>$null
        if ($LASTEXITCODE -eq 0 -and $patchAhead -match '^\d+$') {
            $patch = [int]$patchAhead + 1
        }
    }
} catch {
}

$version = "$versionPrefix.$patch"

try {
    $hash = git rev-parse --short=12 HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($hash)) {
        $commit = $hash.Trim()
    }
} catch {
}

$ldflags = @(
    "-X", "specreport/internal/version.semanticVersion=$version",
    "-X", "specreport/internal/version.commitHash=$commit"
)

if (-not (Test-Path $goversioninfo)) {
    throw "goversioninfo.exe not found: $goversioninfo"
}

$versionParts = $version.Split('.')
if ($versionParts.Count -ne 3) {
    throw "Expected semantic version major.minor.patch, got $version"
}

$verMajor = [int]$versionParts[0]
$verMinor = [int]$versionParts[1]
$verPatch = [int]$versionParts[2]

function New-VersionResource {
    param(
        [string]$VersionInfoPath,
        [string]$OutputPath,
        [string]$Description,
        [string]$InternalName,
        [string]$OriginalName
    )

    if (Test-Path $OutputPath) {
        Remove-Item -LiteralPath $OutputPath -Force
    }
    @'
{
  "FixedFileInfo": {
    "FileVersion": {
      "Major": 0,
      "Minor": 0,
      "Patch": 0,
      "Build": 0
    },
    "ProductVersion": {
      "Major": 0,
      "Minor": 0,
      "Patch": 0,
      "Build": 0
    },
    "FileFlagsMask": "3f",
    "FileFlags ": "00",
    "FileOS": "040004",
    "FileType": "01",
    "FileSubType": "00"
  },
  "StringFileInfo": {
    "Comments": "",
    "CompanyName": "",
    "FileDescription": "",
    "FileVersion": "",
    "InternalName": "",
    "LegalCopyright": "",
    "LegalTrademarks": "",
    "OriginalFilename": "",
    "PrivateBuild": "",
    "ProductName": "",
    "ProductVersion": "",
    "SpecialBuild": ""
  },
  "VarFileInfo": {
    "Translation": {
      "LangID": "0409",
      "CharsetID": "04B0"
    }
  }
}
'@ | Set-Content -LiteralPath $VersionInfoPath -NoNewline

    & $goversioninfo `
        -64 `
        -o $OutputPath `
        -company "Trustfall AB" `
        -product-name "hwreport" `
        -copyright "Copyright (c) Trustfall AB" `
        -description $Description `
        -internal-name $InternalName `
        -original-name $OriginalName `
        -file-version $version `
        -product-version $version `
        -ver-major $verMajor `
        -ver-minor $verMinor `
        -ver-patch $verPatch `
        -ver-build 0 `
        -product-ver-major $verMajor `
        -product-ver-minor $verMinor `
        -product-ver-patch $verPatch `
        -product-ver-build 0 `
        $VersionInfoPath

    if ($LASTEXITCODE -ne 0) {
        throw "goversioninfo failed for $OriginalName"
    }
}

try {
    New-VersionResource -VersionInfoPath $hwreportVersionJSON -OutputPath $hwreportSyso -Description "Hardware inventory collector" -InternalName "hwreport" -OriginalName "hwreport.exe"
    New-VersionResource -VersionInfoPath $hwoverviewVersionJSON -OutputPath $hwoverviewSyso -Description "Hardware overview report generator" -InternalName "hwoverview" -OriginalName "hwoverview.exe"

    go build -trimpath -ldflags ($ldflags -join " ") -o $hwreportOutput ./cmd/hwreport
    go build -trimpath -ldflags ($ldflags -join " ") -o $hwoverviewOutput ./cmd/hwoverview
} finally {
    Remove-Item -LiteralPath $hwreportSyso -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $hwoverviewSyso -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $hwreportVersionJSON -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $hwoverviewVersionJSON -Force -ErrorAction SilentlyContinue
}

Write-Output "Built $hwreportOutput"
Write-Output "Built $hwoverviewOutput"
