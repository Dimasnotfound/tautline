param(
    [Parameter(Mandatory = $true)]
    [string]$AllowedRoots,

    [string]$PublicBaseUrl = "",
    [string]$WidgetDomain = "",
    [string]$TunnelName = "",
    [string]$CustomDomain = "",
    [ValidateSet("off", "quick", "named")]
    [string]$TunnelMode = "off",
    [int]$AgentCapacity = 2,
    [string]$AllowedModels = "auto",
    [switch]$DisableAgents,
    [switch]$Force
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$EnvPath = Join-Path $Root ".env"
$OwnerTokenPath = Join-Path $Root ".owner_token.txt"

if ((Test-Path $EnvPath) -and -not $Force) {
    throw ".env already exists. Use -Force only when you intend to replace it."
}

if ($AgentCapacity -lt 1 -or $AgentCapacity -gt 16) {
    throw "AgentCapacity must be between 1 and 16."
}

$Models = @($AllowedModels.Split(',') | ForEach-Object { $_.Trim() } | Where-Object { $_ })
if ($Models.Count -eq 0) {
    throw "AllowedModels must contain at least one model."
}
$NormalizedModels = ($Models | Select-Object -Unique) -join ','
$DefaultModel = $Models[0]

$Bytes = New-Object byte[] 32
$Random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    $Random.GetBytes($Bytes)
}
finally {
    $Random.Dispose()
}
$OwnerToken = -join ($Bytes | ForEach-Object { $_.ToString("x2") })
$AgentEnabled = (-not $DisableAgents.IsPresent).ToString().ToLowerInvariant()
$TunnelAutoStart = ($TunnelMode -ne "off").ToString().ToLowerInvariant()

$Lines = @(
    "TAUTLINE_ALLOWED_ROOTS=$AllowedRoots",
    "TAUTLINE_OWNER_TOKEN=$OwnerToken",
    "TAUTLINE_REQUIRE_AUTH=true",
    "TAUTLINE_PORT=7688",
    "TAUTLINE_RUNTIME_DIR=runtime/v2",
    "TAUTLINE_OPEN_DASHBOARD=true",
    "TAUTLINE_WIDGETS=on",
    "TAUTLINE_PUBLIC_BASE_URL=$PublicBaseUrl",
    "TAUTLINE_WIDGET_DOMAIN=$WidgetDomain",
    "TAUTLINE_9ROUTER_BASE_URL=http://127.0.0.1:20128/v1",
    "TAUTLINE_9ROUTER_API_KEY=",
    "TAUTLINE_9ROUTER_MODEL=$DefaultModel",
    "TAUTLINE_9ROUTER_ALLOWED_MODELS=$NormalizedModels",
    "TAUTLINE_AGENT_ENABLED=$AgentEnabled",
    "TAUTLINE_AGENT_CAPACITY=$AgentCapacity",
    "TAUTLINE_AGENT_TIMEOUT_SECONDS=900",
    "TAUTLINE_AGENT_IMAGE_SUPPORT=false",
    "TAUTLINE_AGENT_RTK=false",
    "TAUTLINE_AGENT_CAVEMAN=false",
    "TAUTLINE_LIGHTPANDA_PATH=auto",
    "TAUTLINE_LIGHTPANDA_DOCKER_IMAGE=lightpanda/browser:nightly",
    "TAUTLINE_LIGHTPANDA_PORT=9223",
    "TAUTLINE_LIGHTPANDA_AUTOSTART=false",
    "TAUTLINE_LIGHTPANDA_OBEY_ROBOTS=true",
    "TAUTLINE_LIGHTPANDA_NATIVE_MCP=true",
    "TAUTLINE_LIGHTPANDA_PERSIST_SESSION=true",
    "TAUTLINE_LIGHTPANDA_BLOCK_PRIVATE_NETWORKS=true",
    "TAUTLINE_LIGHTPANDA_NATIVE_TIMEOUT_SECONDS=30",
    "TAUTLINE_CLOUDFLARED_PATH=bin/cloudflared.exe",
    "TAUTLINE_TUNNEL_MODE=$TunnelMode",
    "TAUTLINE_TUNNEL_NAME=$TunnelName",
    "TAUTLINE_CUSTOM_DOMAIN=$CustomDomain",
    "TAUTLINE_TUNNEL_PROTOCOL=http2",
    "TAUTLINE_TUNNEL_AUTOSTART=$TunnelAutoStart"
)

$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllLines($EnvPath, $Lines, $Utf8NoBom)
[System.IO.File]::WriteAllText($OwnerTokenPath, $OwnerToken + [Environment]::NewLine, $Utf8NoBom)

Write-Host "Created $EnvPath" -ForegroundColor Green
Write-Host "Created $OwnerTokenPath" -ForegroundColor Green
Write-Host "The owner token was generated locally and was not printed." -ForegroundColor Yellow
Write-Host "Both files are ignored by Git and must remain private." -ForegroundColor DarkGray
