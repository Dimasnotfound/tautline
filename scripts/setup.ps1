param(
    [Parameter(Mandatory = $true)]
    [string]$AllowedRoots,

    [string]$PublicBaseUrl = "",
    [string]$TunnelName = "",
    [switch]$StartTunnel,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$EnvPath = Join-Path $Root ".env"

if ((Test-Path $EnvPath) -and -not $Force) {
    throw ".env already exists. Use -Force only when you intend to replace it."
}

$Bytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($Bytes)
$OwnerToken = [Convert]::ToHexString($Bytes).ToLowerInvariant()

$Lines = @(
    "DEVSPACE_ALLOWED_ROOTS=$AllowedRoots",
    "DEVSPACE_OWNER_TOKEN=$OwnerToken",
    "DEVSPACE_REQUIRE_AUTH=true",
    "DEVSPACE_WIDGETS=changes",
    "DEVSPACE_SHOW_OWNER_TOKEN=false",
    "DEVSPACE_PUBLIC_BASE_URL=$PublicBaseUrl",
    "DEVSPACE_START_TUNNEL=$($StartTunnel.IsPresent.ToString().ToLowerInvariant())",
    "DEVSPACE_TUNNEL_NAME=$TunnelName",
    "DEVSPACE_TUNNEL_PROTOCOL=http2",
    "DEVSPACE_RUNTIME_DIR=runtime"
)

Set-Content -Path $EnvPath -Value $Lines -Encoding UTF8
Write-Host "Created $EnvPath" -ForegroundColor Green
Write-Host "Owner token: $OwnerToken" -ForegroundColor Yellow
Write-Host "Store this token securely. The .env file is ignored by Git." -ForegroundColor DarkGray
