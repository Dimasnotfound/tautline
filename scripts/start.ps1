param(
    [int]$Port = 7676,
    [switch]$Tunnel
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$Binary = Join-Path $Root "bin/devspace.exe"
if (-not (Test-Path $Binary)) {
    & "$PSScriptRoot/build.ps1"
}

& $Binary -start -port $Port -tunnel:$($Tunnel.IsPresent)
