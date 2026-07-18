param(
    [string]$Output = "bin/devspace.exe"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Output) | Out-Null

go fmt ./...
go test ./...
go vet ./...
go build -trimpath -ldflags="-s -w" -o $Output ./cmd/devspace

Write-Host "Built $Output" -ForegroundColor Green
