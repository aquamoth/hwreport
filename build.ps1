$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $repoRoot

$hwreportOutput = Join-Path $repoRoot "hwreport.exe"
$hwoverviewOutput = Join-Path $repoRoot "hwoverview.exe"
$env:GOCACHE = Join-Path $repoRoot ".gocache"
$versionFile = Join-Path $repoRoot "VERSION"

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

go build -trimpath -ldflags ($ldflags -join " ") -o $hwreportOutput ./cmd/hwreport
go build -trimpath -ldflags ($ldflags -join " ") -o $hwoverviewOutput ./cmd/hwoverview

Write-Output "Built $hwreportOutput"
Write-Output "Built $hwoverviewOutput"
