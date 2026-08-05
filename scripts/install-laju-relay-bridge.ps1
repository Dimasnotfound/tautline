param(
    [string]$Port = "",
    [string]$RuntimeDir = "",
    [string]$DestinationRoot = "",
    [switch]$PreflightOnly
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Source = Join-Path $Root "extensions\laju-relay-bridge"
$ExtensionId = "oipiaofdfblejkognebaddegbnfaplph"
$EnvPath = Join-Path $Root ".env"

function Get-DotEnvValue([string]$Path, [string]$Name) {
    if (-not (Test-Path -LiteralPath $Path)) { return "" }
    foreach ($RawLine in [IO.File]::ReadAllLines($Path)) {
        $Line = $RawLine.Trim()
        if (-not $Line -or $Line.StartsWith("#")) { continue }
        $Separator = $Line.IndexOf("=")
        if ($Separator -lt 1 -or $Line.Substring(0, $Separator).Trim() -ne $Name) { continue }
        $Value = $Line.Substring($Separator + 1).Trim()
        if (($Value.StartsWith('"') -and $Value.EndsWith('"')) -or ($Value.StartsWith("'") -and $Value.EndsWith("'"))) {
            $Value = $Value.Substring(1, $Value.Length - 2)
        }
        return $Value
    }
    return ""
}

function Get-ChromeExtensionId([string]$ManifestKey) {
    $PublicKey = [Convert]::FromBase64String($ManifestKey)
    $Hasher = [Security.Cryptography.SHA256]::Create()
    try { $Hash = $Hasher.ComputeHash($PublicKey) } finally { $Hasher.Dispose() }
    $Builder = [Text.StringBuilder]::new(32)
    foreach ($Byte in $Hash[0..15]) {
        [void]$Builder.Append([char]([int][char]'a' + ($Byte -shr 4)))
        [void]$Builder.Append([char]([int][char]'a' + ($Byte -band 15)))
    }
    return $Builder.ToString()
}

function New-BridgeToken {
    $Bytes = New-Object byte[] 32
    $Generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $Generator.GetBytes($Bytes) } finally { $Generator.Dispose() }
    return -join ($Bytes | ForEach-Object { $_.ToString("x2") })
}

function Write-TextAtomically([string]$Path, [string]$Content) {
    $Directory = Split-Path -Parent $Path
    New-Item -ItemType Directory -Force -Path $Directory | Out-Null
    $Temporary = "$Path.tmp-$([guid]::NewGuid().ToString('N'))"
    try {
        [IO.File]::WriteAllText($Temporary, $Content, [Text.UTF8Encoding]::new($false))
        Move-Item -Force -LiteralPath $Temporary -Destination $Path
    }
    finally {
        Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath $Temporary
    }
}

if (-not (Test-Path -LiteralPath (Join-Path $Source "manifest.json"))) {
    throw "Relay Bridge source is incomplete: $Source"
}
$Manifest = Get-Content -LiteralPath (Join-Path $Source "manifest.json") -Raw | ConvertFrom-Json
if ([string]$Manifest.version -ne "2.10.0" -or [int]$Manifest.manifest_version -ne 3) {
    throw "Relay Bridge manifest does not match Tautline v2.10.0."
}
if ((Get-ChromeExtensionId ([string]$Manifest.key)) -ne $ExtensionId) {
    throw "Relay Bridge manifest key does not produce the expected stable extension ID."
}
$Permissions = @($Manifest.host_permissions)
if (($Permissions -contains "<all_urls>") -or -not ($Permissions -contains "https://chatgpt.com/*") -or -not ($Permissions -contains "http://127.0.0.1/*")) {
    throw "Relay Bridge host permissions are not narrowly scoped."
}

if (-not $Port) { $Port = Get-DotEnvValue -Path $EnvPath -Name "TAUTLINE_PORT" }
if (-not $Port) { $Port = "7688" }
$PortNumber = 0
if (-not [int]::TryParse($Port, [ref]$PortNumber) -or $PortNumber -lt 1 -or $PortNumber -gt 65535) {
    throw "Invalid Tautline port: $Port"
}

if (-not $RuntimeDir) { $RuntimeDir = Get-DotEnvValue -Path $EnvPath -Name "TAUTLINE_RUNTIME_DIR" }
if (-not $RuntimeDir) { $RuntimeDir = "runtime\v2" }
if (-not [IO.Path]::IsPathRooted($RuntimeDir)) { $RuntimeDir = Join-Path $Root $RuntimeDir }
$RuntimeDir = [IO.Path]::GetFullPath($RuntimeDir)

if (-not $DestinationRoot) { $DestinationRoot = Join-Path $env:LOCALAPPDATA "Laju Browser\Extensions\unpacked" }
$Destination = Join-Path $DestinationRoot $ExtensionId
$TokenPath = Join-Path $RuntimeDir "state\relay-bridge.token"

if ($PreflightOnly) {
    Write-Host "Relay Bridge source validation passed." -ForegroundColor Green
    Write-Host "Extension ID: $ExtensionId" -ForegroundColor DarkGray
    Write-Host "Destination : $Destination" -ForegroundColor DarkGray
    return
}

$Token = ""
if (Test-Path -LiteralPath $TokenPath) { $Token = [IO.File]::ReadAllText($TokenPath).Trim() }
if ($Token.Length -lt 64) {
    $Token = New-BridgeToken
    Write-TextAtomically -Path $TokenPath -Content ($Token + "`n")
}

New-Item -ItemType Directory -Force -Path $DestinationRoot | Out-Null
$Stage = Join-Path $DestinationRoot (".$ExtensionId.staging-" + [guid]::NewGuid().ToString("N"))
$Backup = Join-Path $DestinationRoot (".$ExtensionId.backup-" + [guid]::NewGuid().ToString("N"))
try {
    New-Item -ItemType Directory -Force -Path $Stage | Out-Null
    foreach ($Name in @("manifest.json", "background.js", "content.js")) {
        Copy-Item -Force -LiteralPath (Join-Path $Source $Name) -Destination (Join-Path $Stage $Name)
    }
    $Config = "self.TAUTLINE_RELAY_CONFIG = Object.freeze({`n  endpoint: " + (("http://127.0.0.1:$PortNumber/relay-bridge/v1") | ConvertTo-Json -Compress) + ",`n  token: " + ($Token | ConvertTo-Json -Compress) + "`n});`n"
    Write-TextAtomically -Path (Join-Path $Stage "config.js") -Content $Config

    if (Test-Path -LiteralPath $Destination) { Move-Item -LiteralPath $Destination -Destination $Backup }
    try {
        Move-Item -LiteralPath $Stage -Destination $Destination
    }
    catch {
        if (Test-Path -LiteralPath $Backup) { Move-Item -LiteralPath $Backup -Destination $Destination }
        throw
    }
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -LiteralPath $Backup
}
finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -LiteralPath $Stage
}

Write-Host "Installed Tautline Relay Bridge v2.10.0 for Laju Browser." -ForegroundColor Green
Write-Host "Extension ID: $ExtensionId" -ForegroundColor DarkGray
Write-Host "Location    : $Destination" -ForegroundColor DarkGray
Write-Host "Restart Laju Browser once to load or refresh the extension." -ForegroundColor Yellow
