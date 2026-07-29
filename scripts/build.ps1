param(
    [string]$Output = "bin/tautline.exe"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

foreach ($Command in @("go", "python", "node")) {
    if (-not (Get-Command $Command -ErrorAction SilentlyContinue)) {
        throw "$Command is required but was not found in PATH."
    }
}

$OutputDirectory = Split-Path -Parent $Output
if ($OutputDirectory) {
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
}

$GoFiles = @(Get-ChildItem -Path . -Recurse -File -Filter "*.go" | ForEach-Object { $_.FullName })
$Unformatted = @(& gofmt -l $GoFiles)
if ($LASTEXITCODE -ne 0) {
    throw "gofmt check failed."
}
if ($Unformatted.Count -gt 0) {
    throw "Go files are not formatted: $($Unformatted -join ', ')"
}

& python -c "import ast,pathlib; ast.parse(pathlib.Path('cmd/tautline/bridge/hermes_skill_bridge.py').read_text(encoding='utf-8'))"
if ($LASTEXITCODE -ne 0) {
    throw "Python bridge syntax check failed."
}

& node --check cmd/tautline/web/app.js
if ($LASTEXITCODE -ne 0) {
    throw "Dashboard JavaScript syntax check failed."
}

$SavedWidgetDomain = [Environment]::GetEnvironmentVariable("TAUTLINE_WIDGET_DOMAIN", "Process")
$SavedPublicBaseUrl = [Environment]::GetEnvironmentVariable("TAUTLINE_PUBLIC_BASE_URL", "Process")
try {
    [Environment]::SetEnvironmentVariable("TAUTLINE_WIDGET_DOMAIN", $null, "Process")
    [Environment]::SetEnvironmentVariable("TAUTLINE_PUBLIC_BASE_URL", $null, "Process")
    & go test -count=1 ./...
    if ($LASTEXITCODE -ne 0) {
        throw "Go tests failed."
    }
}
finally {
    [Environment]::SetEnvironmentVariable("TAUTLINE_WIDGET_DOMAIN", $SavedWidgetDomain, "Process")
    [Environment]::SetEnvironmentVariable("TAUTLINE_PUBLIC_BASE_URL", $SavedPublicBaseUrl, "Process")
}

& go vet ./...
if ($LASTEXITCODE -ne 0) {
    throw "go vet failed."
}

& go build -trimpath -ldflags="-s -w" -o $Output ./cmd/tautline
if ($LASTEXITCODE -ne 0) {
    throw "Tautline build failed."
}

$ShimOutput = "bin/lightpanda-shim.exe"
& go build -trimpath -ldflags="-s -w" -o $ShimOutput ./cmd/lightpanda-shim
if ($LASTEXITCODE -ne 0) {
    throw "Lightpanda shim build failed."
}

Write-Host "Quality gates passed." -ForegroundColor Green
Write-Host "Built $Output" -ForegroundColor Green
Write-Host "Built $ShimOutput" -ForegroundColor Green
