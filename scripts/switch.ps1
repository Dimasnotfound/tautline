param(
    [string]$OldRoot = "D:\PRIMACODES (ME)\chatgpt-mcp",
    [string]$Port = "",
    [ValidateRange(0, 30)]
    [int]$HandoffDelaySeconds = 0,
    [switch]$UseExistingStage,
    [switch]$PreflightOnly
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$BuildScript = Join-Path $PSScriptRoot "build.ps1"
$StartScript = Join-Path $PSScriptRoot "start.ps1"
$EnvPath = Join-Path $Root ".env"
$ExpectedVersion = "2.8.1"

$Binary = Join-Path $Root "bin\tautline.exe"
$Shim = Join-Path $Root "bin\lightpanda-shim.exe"
$StageBinary = Join-Path $Root "bin\tautline.next.exe"
$StageShim = Join-Path $Root "bin\lightpanda-shim.next.exe"
$BackupBinary = Join-Path $Root "bin\tautline.previous.exe"
$BackupShim = Join-Path $Root "bin\lightpanda-shim.previous.exe"

function Get-DotEnvValue {
    param(
        [string]$Path,
        [string]$Name
    )

    if (-not (Test-Path $Path)) {
        return ""
    }
    foreach ($RawLine in [System.IO.File]::ReadAllLines($Path)) {
        $Line = $RawLine.Trim()
        if (-not $Line -or $Line.StartsWith("#")) {
            continue
        }
        $Separator = $Line.IndexOf("=")
        if ($Separator -lt 1) {
            continue
        }
        $Key = $Line.Substring(0, $Separator).Trim()
        if ($Key -ne $Name) {
            continue
        }
        $Value = $Line.Substring($Separator + 1).Trim()
        if (($Value.StartsWith([char]34) -and $Value.EndsWith([char]34)) -or ($Value.StartsWith("'") -and $Value.EndsWith("'"))) {
            $Value = $Value.Substring(1, $Value.Length - 2)
        }
        return $Value
    }
    return ""
}

function Stop-TautlineBinary {
    param(
        [string]$Executable,
        [string]$SelectedPort
    )

    if (-not (Test-Path $Executable)) {
        return
    }
    try {
        & $Executable -stop -port $SelectedPort 2>$null | Out-Null
    }
    catch {
        # The process scan below handles stale PID state and older stop behavior.
    }
}

function Stop-ManagedProcesses {
    param([string[]]$Roots)

    $NormalizedRoots = @($Roots | Where-Object { $_ -and (Test-Path $_) } | ForEach-Object {
        ([System.IO.Path]::GetFullPath($_)).TrimEnd('\').ToLowerInvariant()
    } | Select-Object -Unique)
    if ($NormalizedRoots.Count -eq 0) {
        return
    }

    $ManagedNames = @("tautline.exe", "lightpanda-shim.exe", "cloudflared.exe")
    $Processes = @(Get-CimInstance Win32_Process | Where-Object {
        if (-not $_.ExecutablePath -or -not ($ManagedNames -contains $_.Name.ToLowerInvariant())) {
            return $false
        }
        $Executable = $_.ExecutablePath.ToLowerInvariant()
        foreach ($ManagedRoot in $NormalizedRoots) {
            if ($Executable.StartsWith($ManagedRoot + "\")) {
                return $true
            }
        }
        return $false
    })

    foreach ($Process in $Processes) {
        Stop-Process -Id $Process.ProcessId -Force -ErrorAction SilentlyContinue
    }
}

function Get-ActiveTautlineRuntime {
    param([string]$SelectedPort)

    try {
        $Listener = Get-NetTCPConnection -LocalPort ([int]$SelectedPort) -State Listen -ErrorAction Stop | Select-Object -First 1
        if (-not $Listener) {
            return $null
        }
        $Process = Get-CimInstance Win32_Process -Filter "ProcessId = $($Listener.OwningProcess)" -ErrorAction Stop
        if (-not $Process -or $Process.Name -ne "tautline.exe" -or -not $Process.ExecutablePath) {
            return $null
        }
        $Version = ""
        try {
            $Health = Invoke-RestMethod -Uri "http://127.0.0.1:$SelectedPort/healthz" -TimeoutSec 2
            $Version = [string]$Health.version
        }
        catch {}
        return [pscustomobject]@{
            ProcessId = [int]$Process.ProcessId
            Executable = [System.IO.Path]::GetFullPath([string]$Process.ExecutablePath)
            WorkingDirectory = Split-Path -Parent ([System.IO.Path]::GetFullPath([string]$Process.ExecutablePath))
            CommandLine = [string]$Process.CommandLine
            Version = $Version
        }
    }
    catch {
        return $null
    }
}

function Wait-ForPortToStop {
    param([string]$HealthUrl)

    for ($Attempt = 0; $Attempt -lt 30; $Attempt++) {
        try {
            Invoke-RestMethod -Uri $HealthUrl -TimeoutSec 1 | Out-Null
            Start-Sleep -Milliseconds 500
        }
        catch {
            return
        }
    }
    throw "The previous Tautline instance did not release the selected port."
}

function Start-TautlineConsole {
    param([string]$SelectedPort)

    $Arguments = "-NoLogo -NoProfile -ExecutionPolicy Bypass -NoExit -File `"$StartScript`" -Port `"$SelectedPort`""
    return Start-Process powershell.exe -ArgumentList $Arguments -WorkingDirectory $Root -PassThru
}

function Wait-ForHealthyVersion {
    param(
        [string]$HealthUrl,
        [string]$Version,
        [System.Diagnostics.Process]$Process
    )

    for ($Attempt = 0; $Attempt -lt 90; $Attempt++) {
        if ($Process.HasExited) {
            throw "The new Tautline process exited before the health check succeeded."
        }
        try {
            $Health = Invoke-RestMethod -Uri $HealthUrl -TimeoutSec 2
            if ($Health.service -eq "Tautline" -and $Health.version -eq $Version) {
                return $Health
            }
        }
        catch {
            # The process may still be starting.
        }
        Start-Sleep -Seconds 1
    }
    throw "Tautline v$Version did not become healthy at $HealthUrl."
}

function Get-FreeLocalPort {
    $Listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try {
        $Listener.Start()
        return ([System.Net.IPEndPoint]$Listener.LocalEndpoint).Port
    }
    finally {
        $Listener.Stop()
    }
}

function ConvertTo-Base64Url {
    param([byte[]]$Bytes)

    return [Convert]::ToBase64String($Bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
}

function Get-QueryValue {
    param(
        [System.Uri]$Uri,
        [string]$Name
    )

    foreach ($Pair in $Uri.Query.TrimStart("?").Split("&", [System.StringSplitOptions]::RemoveEmptyEntries)) {
        $Parts = $Pair.Split("=", 2)
        $Key = [System.Uri]::UnescapeDataString($Parts[0].Replace("+", " "))
        if ($Key -ne $Name) {
            continue
        }
        $Value = if ($Parts.Count -gt 1) { $Parts[1] } else { "" }
        return [System.Uri]::UnescapeDataString($Value.Replace("+", " "))
    }
    return ""
}

function New-FormContent {
    param([hashtable]$Values)

    $Dictionary = [System.Collections.Generic.Dictionary[string,string]]::new()
    foreach ($Entry in $Values.GetEnumerator()) {
        $Dictionary[[string]$Entry.Key] = [string]$Entry.Value
    }
    return [System.Net.Http.FormUrlEncodedContent]::new($Dictionary)
}

function Test-StagedOAuthFlow {
    param(
        [string]$BaseUrl,
        [string]$OwnerToken,
        [string]$RuntimeRoot
    )

    Add-Type -AssemblyName System.Net.Http -ErrorAction Stop

    if ([string]::IsNullOrWhiteSpace($OwnerToken)) {
        throw "TAUTLINE_OWNER_TOKEN is required for the staged ChatGPT OAuth preflight."
    }

    $Metadata = Invoke-RestMethod -Uri "$BaseUrl/.well-known/oauth-authorization-server" -TimeoutSec 5
    $Scopes = @($Metadata.scopes_supported)
    if (-not ($Scopes -contains "tautline") -or -not ($Scopes -contains "offline_access")) {
        throw "Staged OAuth metadata does not advertise tautline and offline_access."
    }
    if ($Metadata.registration_endpoint -ne "$BaseUrl/register") {
        throw "Staged OAuth metadata returned an unexpected registration endpoint."
    }

    foreach ($AuthorizationMetadataPath in @(
        "/.well-known/oauth-authorization-server/mcp",
        "/.well-known/oauth-authorization-server/mcp/v2",
        "/mcp/.well-known/oauth-authorization-server",
        "/mcp/v2/.well-known/oauth-authorization-server",
        "/.well-known/openid-configuration",
        "/.well-known/openid-configuration/mcp",
        "/.well-known/openid-configuration/mcp/v2",
        "/mcp/.well-known/openid-configuration",
        "/mcp/v2/.well-known/openid-configuration"
    )) {
        $CompatibleMetadata = Invoke-RestMethod -Uri "$BaseUrl$AuthorizationMetadataPath" -TimeoutSec 5
        if ([string]$CompatibleMetadata.issuer -ne $BaseUrl -or [string]$CompatibleMetadata.registration_endpoint -ne "$BaseUrl/register") {
            throw "Staged OAuth metadata compatibility route $AuthorizationMetadataPath returned inconsistent configuration."
        }
    }

    foreach ($ResourceMetadataPath in @(
        "/.well-known/oauth-protected-resource",
        "/.well-known/oauth-protected-resource/mcp",
        "/mcp/.well-known/oauth-protected-resource"
    )) {
        $ResourceMetadata = Invoke-RestMethod -Uri "$BaseUrl$ResourceMetadataPath" -TimeoutSec 5
        if ([string]$ResourceMetadata.resource -ne "$BaseUrl/mcp") {
            throw "Staged OAuth protected resource metadata at $ResourceMetadataPath returned the wrong MCP resource."
        }
        if (@($ResourceMetadata.authorization_servers).Count -ne 1 -or [string]$ResourceMetadata.authorization_servers[0] -ne $BaseUrl) {
            throw "Staged OAuth protected resource metadata at $ResourceMetadataPath returned the wrong authorization server."
        }
        $ResourceScopes = @($ResourceMetadata.scopes_supported)
        if (-not ($ResourceScopes -contains "tautline") -or -not ($ResourceScopes -contains "offline_access")) {
            throw "Staged OAuth protected resource metadata at $ResourceMetadataPath does not advertise tautline and offline_access."
        }
    }

    foreach ($ResourceMetadataPath in @(
        "/.well-known/oauth-protected-resource/mcp/v2",
        "/mcp/v2/.well-known/oauth-protected-resource"
    )) {
        $ResourceMetadata = Invoke-RestMethod -Uri "$BaseUrl$ResourceMetadataPath" -TimeoutSec 5
        if ([string]$ResourceMetadata.resource -ne "$BaseUrl/mcp/v2") {
            throw "Staged OAuth protected resource metadata at $ResourceMetadataPath returned the wrong versioned MCP resource."
        }
        $ResourceScopes = @($ResourceMetadata.scopes_supported)
        if (-not ($ResourceScopes -contains "tautline") -or -not ($ResourceScopes -contains "offline_access")) {
            throw "Staged OAuth protected resource metadata at $ResourceMetadataPath does not advertise tautline and offline_access."
        }
    }

    $ChallengeHandler = [System.Net.Http.HttpClientHandler]::new()
    $ChallengeClient = [System.Net.Http.HttpClient]::new($ChallengeHandler)
    try {
        foreach ($ChallengeCase in @(
            [pscustomobject]@{ Path = "/mcp"; MetadataPath = "/.well-known/oauth-protected-resource/mcp" },
            [pscustomobject]@{ Path = "/mcp/v2"; MetadataPath = "/.well-known/oauth-protected-resource/mcp/v2" }
        )) {
            $ChallengeRequest = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Post, "$BaseUrl$($ChallengeCase.Path)")
            $ChallengeRequest.Headers.TryAddWithoutValidation("Accept", "application/json, text/event-stream") | Out-Null
            $ChallengeRequest.Content = [System.Net.Http.StringContent]::new(
                '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"tautline-oauth-discovery-preflight","version":"1.0"}}}',
                [System.Text.Encoding]::UTF8,
                "application/json"
            )
            $ChallengeResponse = $null
            try {
                $ChallengeResponse = $ChallengeClient.SendAsync($ChallengeRequest).GetAwaiter().GetResult()
                if ([int]$ChallengeResponse.StatusCode -ne 401) {
                    throw "Staged unauthenticated MCP request to $($ChallengeCase.Path) returned HTTP $([int]$ChallengeResponse.StatusCode) instead of 401."
                }
                $ChallengeValues = @($ChallengeResponse.Headers.GetValues("WWW-Authenticate"))
                $Challenge = $ChallengeValues -join ", "
                $ExpectedResourceMetadata = "resource_metadata=`"$BaseUrl$($ChallengeCase.MetadataPath)`""
                if (-not $Challenge.StartsWith("Bearer ") -or -not $Challenge.Contains($ExpectedResourceMetadata) -or -not $Challenge.Contains('scope="tautline"')) {
                    throw "Staged MCP challenge at $($ChallengeCase.Path) does not advertise the expected protected resource metadata endpoint."
                }
            }
            finally {
                if ($ChallengeResponse) { $ChallengeResponse.Dispose() }
                $ChallengeRequest.Dispose()
            }
        }
    }
    finally {
        $ChallengeClient.Dispose()
        $ChallengeHandler.Dispose()
    }

    $ChatGPTRegistrationBody = @{
        client_name = "ChatGPT"
        redirect_uris = @("https://chatgpt.com/connector/oauth/tautline-preflight")
        grant_types = @("authorization_code", "refresh_token")
        response_types = @("code")
        token_endpoint_auth_method = "none"
        scope = "tautline offline_access"
    } | ConvertTo-Json -Depth 5 -Compress
    $ChatGPTRegistration = Invoke-RestMethod -Uri "$BaseUrl/register" -Method Post -ContentType "application/json" -Body $ChatGPTRegistrationBody -TimeoutSec 5
    if ([string]$ChatGPTRegistration.client_id -ne "chatgpt.com" -or [string]$ChatGPTRegistration.scope -ne "tautline offline_access") {
        throw "Staged OAuth registration did not preserve the v2.4-compatible ChatGPT client contract."
    }

    $CallbackPort = Get-FreeLocalPort
    $Callback = "http://127.0.0.1:$CallbackPort/callback"
    $RegistrationBody = @{
        client_name = "Tautline staged ChatGPT probe"
        redirect_uris = @($Callback)
        grant_types = @("authorization_code", "refresh_token")
        response_types = @("code")
        token_endpoint_auth_method = "none"
        scope = "tautline offline_access"
    } | ConvertTo-Json -Depth 5 -Compress
    $Registration = Invoke-RestMethod -Uri "$BaseUrl/register" -Method Post -ContentType "application/json" -Body $RegistrationBody -TimeoutSec 5
    if ([string]::IsNullOrWhiteSpace([string]$Registration.client_id) -or [string]$Registration.client_id -eq "chatgpt.com") {
        throw "Staged OAuth dynamic registration did not return a unique client_id."
    }
    if (@($Registration.redirect_uris).Count -ne 1 -or [string]$Registration.redirect_uris[0] -ne $Callback) {
        throw "Staged OAuth registration did not preserve the exact callback URI."
    }

    $VerifierBytes = New-Object byte[] 48
    $Random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $Random.GetBytes($VerifierBytes)
    }
    finally {
        $Random.Dispose()
    }
    $Verifier = ConvertTo-Base64Url -Bytes $VerifierBytes
    $SHA256 = [System.Security.Cryptography.SHA256]::Create()
    try {
        $Challenge = ConvertTo-Base64Url -Bytes ($SHA256.ComputeHash([System.Text.Encoding]::ASCII.GetBytes($Verifier)))
    }
    finally {
        $SHA256.Dispose()
    }

    $Resource = "$BaseUrl/mcp/v2"
    $State = "tautline-preflight-state"
    $AuthorizeValues = [ordered]@{
        response_type = "code"
        client_id = [string]$Registration.client_id
        redirect_uri = $Callback
        state = $State
        scope = "tautline offline_access"
        resource = $Resource
        code_challenge = $Challenge
        code_challenge_method = "S256"
    }
    $Query = @($AuthorizeValues.GetEnumerator() | ForEach-Object {
        [System.Uri]::EscapeDataString([string]$_.Key) + "=" + [System.Uri]::EscapeDataString([string]$_.Value)
    }) -join "&"
    $ApprovalPage = Invoke-WebRequest -Uri "$BaseUrl/authorize?$Query" -UseBasicParsing -TimeoutSec 5
    if ($ApprovalPage.StatusCode -ne 200 -or -not $ApprovalPage.Content.Contains('name="resource"')) {
        throw "Staged OAuth authorization page did not preserve the MCP resource."
    }

    $Handler = [System.Net.Http.HttpClientHandler]::new()
    $Handler.AllowAutoRedirect = $false
    $Client = [System.Net.Http.HttpClient]::new($Handler)
    try {
        $ApprovalValues = @{}
        foreach ($Entry in $AuthorizeValues.GetEnumerator()) {
            $ApprovalValues[[string]$Entry.Key] = [string]$Entry.Value
        }
        $ApprovalValues.token = $OwnerToken
        $ApprovalContent = New-FormContent -Values $ApprovalValues
        try {
            $ApprovalResponse = $Client.PostAsync("$BaseUrl/authorize", $ApprovalContent).GetAwaiter().GetResult()
        }
        finally {
            $ApprovalContent.Dispose()
        }
        if ([int]$ApprovalResponse.StatusCode -ne 302 -or -not $ApprovalResponse.Headers.Location) {
            throw "Staged OAuth owner approval did not return the ChatGPT callback redirect."
        }
        $Redirect = if ($ApprovalResponse.Headers.Location.IsAbsoluteUri) {
            $ApprovalResponse.Headers.Location
        } else {
            [System.Uri]::new([System.Uri]$Callback, $ApprovalResponse.Headers.Location)
        }
        $Code = Get-QueryValue -Uri $Redirect -Name "code"
        if ([string]::IsNullOrWhiteSpace($Code) -or (Get-QueryValue -Uri $Redirect -Name "state") -ne $State) {
            throw "Staged OAuth authorization redirect is missing a valid code or state."
        }
    }
    finally {
        $Client.Dispose()
        $Handler.Dispose()
    }

    $Token = Invoke-RestMethod -Uri "$BaseUrl/token" -Method Post -ContentType "application/x-www-form-urlencoded" -Body @{
        grant_type = "authorization_code"
        code = $Code
        client_id = [string]$Registration.client_id
        redirect_uri = $Callback
        code_verifier = $Verifier
        resource = $Resource
    } -TimeoutSec 5
    if ([string]::IsNullOrWhiteSpace([string]$Token.access_token) -or [string]::IsNullOrWhiteSpace([string]$Token.refresh_token) -or [string]$Token.scope -ne "tautline offline_access") {
        throw "Staged OAuth authorization-code exchange returned an incomplete token response."
    }

    $Refreshed = Invoke-RestMethod -Uri "$BaseUrl/token" -Method Post -ContentType "application/x-www-form-urlencoded" -Body @{
        grant_type = "refresh_token"
        refresh_token = [string]$Token.refresh_token
        client_id = [string]$Registration.client_id
        resource = $Resource
    } -TimeoutSec 5
    if ([string]::IsNullOrWhiteSpace([string]$Refreshed.access_token) -or [string]::IsNullOrWhiteSpace([string]$Refreshed.refresh_token)) {
        throw "Staged OAuth refresh-token exchange failed."
    }

    $Initialize = Invoke-RestMethod -Uri "$BaseUrl/mcp/v2" -Method Post -ContentType "application/json" -Headers @{
        Accept = "application/json, text/event-stream"
        Authorization = "Bearer $($Refreshed.access_token)"
    } -Body (@{
        jsonrpc = "2.0"
        id = 1
        method = "initialize"
        params = @{
            protocolVersion = "2025-06-18"
            capabilities = @{}
            clientInfo = @{ name = "tautline-switch-preflight"; version = "1.0" }
        }
    } | ConvertTo-Json -Depth 8 -Compress) -TimeoutSec 10
    if ([string]$Initialize.result.serverInfo.version -ne $ExpectedVersion) {
        throw "Staged authenticated MCP initialization returned the wrong server version."
    }

    $ClientState = Join-Path $RuntimeRoot "state\oauth-clients.json"
    if (-not (Test-Path $ClientState)) {
        throw "Staged OAuth dynamic registration was not persisted."
    }
}

function Test-StagedRuntime {
    param(
        [string]$Executable,
        [string]$Version
    )

    $PreflightPort = Get-FreeLocalPort
    $PreflightRoot = Join-Path $Root "runtime\v2\update-preflight"
    $PreflightConfigDirectory = Join-Path $PreflightRoot "config"
    $PreflightConfig = Join-Path $PreflightConfigDirectory "tautline.json"
    $UpdateLogDirectory = Join-Path $Root "runtime\v2\update"
    $StandardOutput = Join-Path $UpdateLogDirectory "preflight.stdout.log"
    $StandardError = Join-Path $UpdateLogDirectory "preflight.stderr.log"
    $SourceConfig = if ($env:TAUTLINE_CONFIG) { $env:TAUTLINE_CONFIG } else { Join-Path $Root "runtime\v2\config\tautline.json" }

    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $PreflightRoot
    New-Item -ItemType Directory -Force -Path $PreflightConfigDirectory, $UpdateLogDirectory | Out-Null

    $Config = if (Test-Path $SourceConfig) {
        Get-Content -Raw $SourceConfig | ConvertFrom-Json
    } else {
        [pscustomobject]@{}
    }
    $SourceRuntime = if ($Config.runtime_dir) {
        if ([System.IO.Path]::IsPathRooted([string]$Config.runtime_dir)) { [string]$Config.runtime_dir } else { Join-Path $Root ([string]$Config.runtime_dir) }
    } else {
        Join-Path $Root "runtime\v2"
    }

    $Config | Add-Member -NotePropertyName port -NotePropertyValue ([string]$PreflightPort) -Force
    $Config | Add-Member -NotePropertyName runtime_dir -NotePropertyValue $PreflightRoot -Force
    $Config | Add-Member -NotePropertyName open_dashboard -NotePropertyValue $false -Force
    $Config | Add-Member -NotePropertyName agent_enabled -NotePropertyValue $false -Force
    if (-not $Config.tunnel) { $Config | Add-Member -NotePropertyName tunnel -NotePropertyValue ([pscustomobject]@{}) -Force }
    $Config.tunnel | Add-Member -NotePropertyName mode -NotePropertyValue "off" -Force
    $Config.tunnel | Add-Member -NotePropertyName auto_start -NotePropertyValue $false -Force
    if (-not $Config.lightpanda) { $Config | Add-Member -NotePropertyName lightpanda -NotePropertyValue ([pscustomobject]@{}) -Force }
    $Config.lightpanda | Add-Member -NotePropertyName auto_start -NotePropertyValue $false -Force
    $Config.lightpanda | Add-Member -NotePropertyName native_mcp -NotePropertyValue $false -Force

    $NativeGoogleDocsEnabled = [bool]($Config.google_docs -and $Config.google_docs.enabled)
    foreach ($Server in @($Config.mcp_servers)) {
        if ($Server.id -eq "google_docs" -and [string]$Server.url -match '^https://docsmcp\.googleapis\.com/') {
            $NativeGoogleDocsEnabled = $NativeGoogleDocsEnabled -or [bool]$Server.enabled
        }
    }
    $EnabledMCPCount = @($Config.mcp_servers | Where-Object {
        $_.enabled -and -not ($_.id -eq "google_docs" -and [string]$_.url -match '^https://docsmcp\.googleapis\.com/')
    }).Count
    foreach ($Server in @($Config.mcp_servers)) {
        if (-not $Server.oauth) { continue }
        $RelativeToken = if ($Server.oauth.token_file) { [string]$Server.oauth.token_file } else { Join-Path "oauth" ("$($Server.id).json") }
        if ([System.IO.Path]::IsPathRooted($RelativeToken) -or $RelativeToken.Contains("..")) {
            throw "MCP OAuth token path is not safe for preflight: $RelativeToken"
        }
        $SourceToken = Join-Path $SourceRuntime $RelativeToken
        $DestinationToken = Join-Path $PreflightRoot $RelativeToken
        if (Test-Path $SourceToken) {
            New-Item -ItemType Directory -Force -Path (Split-Path -Parent $DestinationToken) | Out-Null
            Copy-Item -Force $SourceToken $DestinationToken
        }
    }
    if ($Config.google_docs -and $Config.google_docs.oauth) {
        $RelativeToken = if ($Config.google_docs.oauth.token_file) { [string]$Config.google_docs.oauth.token_file } else { "oauth\google_docs.json" }
        if ([System.IO.Path]::IsPathRooted($RelativeToken) -or $RelativeToken.Contains("..")) {
            throw "Google Docs OAuth token path is not safe for preflight: $RelativeToken"
        }
        $SourceToken = Join-Path $SourceRuntime $RelativeToken
        $DestinationToken = Join-Path $PreflightRoot $RelativeToken
        if (Test-Path $SourceToken) {
            New-Item -ItemType Directory -Force -Path (Split-Path -Parent $DestinationToken) | Out-Null
            Copy-Item -Force $SourceToken $DestinationToken
        }
    }
    $ConfigJSON = $Config | ConvertTo-Json -Depth 20
    [System.IO.File]::WriteAllText($PreflightConfig, $ConfigJSON, [System.Text.UTF8Encoding]::new($false))

    $PreflightOwnerToken = [string]$env:TAUTLINE_OWNER_TOKEN
    if ([string]::IsNullOrWhiteSpace($PreflightOwnerToken)) {
        $PreflightOwnerToken = Get-DotEnvValue -Path $EnvPath -Name "TAUTLINE_OWNER_TOKEN"
    }
    if ([string]::IsNullOrWhiteSpace($PreflightOwnerToken)) {
        $PreflightOwnerToken = [string]$env:DEVSPACE_OWNER_TOKEN
    }
    if ([string]::IsNullOrWhiteSpace($PreflightOwnerToken)) {
        $PreflightOwnerToken = Get-DotEnvValue -Path (Join-Path $OldRoot ".env") -Name "DEVSPACE_OWNER_TOKEN"
    }
    if ([string]::IsNullOrWhiteSpace($PreflightOwnerToken)) {
        foreach ($Candidate in @(Join-Path $Root ".owner_token.txt", Join-Path $OldRoot ".owner_token.txt")) {
            if (Test-Path $Candidate) {
                $PreflightOwnerToken = [System.IO.File]::ReadAllText($Candidate).Trim()
                if (-not [string]::IsNullOrWhiteSpace($PreflightOwnerToken)) {
                    break
                }
            }
        }
    }

    $PreflightBaseUrl = "http://127.0.0.1:$PreflightPort"
    $SavedEnvironment = @{}
    $PreflightEnvironment = @{
        TAUTLINE_CONFIG = $PreflightConfig
        TAUTLINE_RUNTIME_DIR = $PreflightRoot
        TAUTLINE_PORT = [string]$PreflightPort
        TAUTLINE_OWNER_TOKEN = $PreflightOwnerToken
        TAUTLINE_REQUIRE_AUTH = "true"
        TAUTLINE_PUBLIC_BASE_URL = $PreflightBaseUrl
        TAUTLINE_OPEN_DASHBOARD = "false"
        TAUTLINE_TUNNEL_MODE = "off"
        TAUTLINE_TUNNEL_AUTOSTART = "false"
        TAUTLINE_LIGHTPANDA_AUTOSTART = "false"
        TAUTLINE_LIGHTPANDA_NATIVE_MCP = "false"
        TAUTLINE_AGENT_ENABLED = "false"
    }
    foreach ($Entry in $PreflightEnvironment.GetEnumerator()) {
        $SavedEnvironment[$Entry.Key] = [Environment]::GetEnvironmentVariable($Entry.Key, "Process")
        [Environment]::SetEnvironmentVariable($Entry.Key, $Entry.Value, "Process")
    }

    $Process = $null
    try {
        Remove-Item -Force -ErrorAction SilentlyContinue $StandardOutput, $StandardError
        $Process = Start-Process $Executable -ArgumentList @("-start", "-port", [string]$PreflightPort, "-dashboard=false") -WorkingDirectory $Root -RedirectStandardOutput $StandardOutput -RedirectStandardError $StandardError -PassThru
        $Health = Wait-ForHealthyVersion -HealthUrl "http://127.0.0.1:$PreflightPort/healthz" -Version $Version -Process $Process
        if ($Health.host_instructions.fallback) {
            throw "The staged runtime fell back from the configured Codex host instructions. See $StandardError"
        }
        if ($EnabledMCPCount -gt 0 -and [int]$Health.mcp_clients.connected -lt $EnabledMCPCount) {
            throw "Staged runtime connected $($Health.mcp_clients.connected) of $EnabledMCPCount enabled MCP integrations. See $StandardError"
        }
        if ($NativeGoogleDocsEnabled -and (-not $Health.google_docs.enabled -or [string]$Health.google_docs.mode -ne "native-rest")) {
            throw "Staged runtime did not activate native Google Docs REST integration. See $StandardError"
        }
        Test-StagedOAuthFlow -BaseUrl $PreflightBaseUrl -OwnerToken $PreflightOwnerToken -RuntimeRoot $PreflightRoot
        Write-Host "Staged runtime preflight passed on temporary port $PreflightPort." -ForegroundColor Green
        Write-Host "ChatGPT OAuth: v2.4-compatible registration, full discovery aliases, endpoint-specific challenges, PKCE, refresh, /mcp/v2 initialize, and persisted loopback client state passed." -ForegroundColor DarkGray
        Write-Host "Host instructions: $($Health.host_instructions.source), loaded=$($Health.host_instructions.loaded)" -ForegroundColor DarkGray
    }
    finally {
        if ($Process) {
            try { & taskkill.exe /PID $Process.Id /T /F 2>$null | Out-Null } catch {}
            try { $Process.WaitForExit(5000) | Out-Null } catch {}
        }
        foreach ($Entry in $PreflightEnvironment.GetEnumerator()) {
            [Environment]::SetEnvironmentVariable($Entry.Key, $SavedEnvironment[$Entry.Key], "Process")
        }
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $PreflightRoot
    }
}

if (-not $Port) {
    $Port = Get-DotEnvValue -Path $EnvPath -Name "TAUTLINE_PORT"
}
if (-not $Port) {
    $Port = "7688"
}
$HealthUrl = "http://127.0.0.1:$Port/healthz"
$PreviousRuntime = Get-ActiveTautlineRuntime -SelectedPort $Port

Set-Location $Root
Remove-Item -Force -ErrorAction SilentlyContinue $BackupBinary, $BackupShim
$BuildExitCode = 0
if ($UseExistingStage) {
    Write-Host "Using the previously validated staged binaries." -ForegroundColor Cyan
} else {
    Remove-Item -Force -ErrorAction SilentlyContinue $StageBinary, $StageShim
    Write-Host "Building and validating Tautline v$ExpectedVersion before handoff..." -ForegroundColor Cyan
    & $BuildScript -Output "bin/tautline.next.exe" -ShimOutput "bin/lightpanda-shim.next.exe"
    $BuildExitCode = $LASTEXITCODE
}
if ($BuildExitCode -ne 0 -or -not (Test-Path $StageBinary) -or -not (Test-Path $StageShim)) {
    throw "The staged Tautline build did not complete successfully."
}

Write-Host "Running isolated runtime and MCP preflight before stopping the active version..." -ForegroundColor Cyan
Test-StagedRuntime -Executable $StageBinary -Version $ExpectedVersion
if ($PreflightOnly) {
    Remove-Item -Force -ErrorAction SilentlyContinue $StageBinary, $StageShim
    Write-Host "Preflight-only validation completed. The active Tautline process was not changed." -ForegroundColor Green
    return
}

if ($HandoffDelaySeconds -gt 0) {
    Start-Sleep -Seconds $HandoffDelaySeconds
}

$InstalledNewBinary = $false
try {
    Write-Host "Stopping previous Tautline processes..." -ForegroundColor Yellow
    Stop-TautlineBinary -Executable $Binary -SelectedPort $Port
    Stop-TautlineBinary -Executable (Join-Path $OldRoot "bin\tautline.exe") -SelectedPort $Port
    Stop-TautlineBinary -Executable (Join-Path $OldRoot "versions\v2.4.0\bin\tautline.exe") -SelectedPort $Port
    Start-Sleep -Seconds 1
    Stop-ManagedProcesses -Roots @($Root, $OldRoot)
    Wait-ForPortToStop -HealthUrl $HealthUrl

    if (Test-Path $Binary) {
        Move-Item -Force $Binary $BackupBinary
    }
    if (Test-Path $Shim) {
        Move-Item -Force $Shim $BackupShim
    }
    Move-Item -Force $StageBinary $Binary
    Move-Item -Force $StageShim $Shim
    $InstalledNewBinary = $true

    Write-Host "Starting Tautline v$ExpectedVersion..." -ForegroundColor Cyan
    $NewProcess = Start-TautlineConsole -SelectedPort $Port
    $Health = Wait-ForHealthyVersion -HealthUrl $HealthUrl -Version $ExpectedVersion -Process $NewProcess

    Remove-Item -Force -ErrorAction SilentlyContinue $BackupBinary, $BackupShim
    Write-Host "Update handoff completed successfully." -ForegroundColor Green
    Write-Host "Active version : $($Health.version)" -ForegroundColor Green
    Write-Host "Active folder  : $Root" -ForegroundColor Green
    Write-Host "Dashboard      : http://127.0.0.1:$Port/" -ForegroundColor Green
}
catch {
    $Failure = $_
    Write-Warning "The new build could not be activated. Restoring the previous binaries."
    if ($InstalledNewBinary) {
        Stop-TautlineBinary -Executable $Binary -SelectedPort $Port
        Start-Sleep -Seconds 1
        Stop-ManagedProcesses -Roots @($Root)
        Remove-Item -Force -ErrorAction SilentlyContinue $Binary, $Shim
    }
    if (Test-Path $BackupBinary) {
        Move-Item -Force $BackupBinary $Binary
    }
    if (Test-Path $BackupShim) {
        Move-Item -Force $BackupShim $Shim
    }
    $RollbackExecutable = $Binary
    $RollbackWorkingDirectory = $Root
    $RollbackVersion = ""
    if ($PreviousRuntime -and (Test-Path $PreviousRuntime.Executable)) {
        $RollbackExecutable = [string]$PreviousRuntime.Executable
        $RollbackWorkingDirectory = [string]$PreviousRuntime.WorkingDirectory
        $RollbackVersion = [string]$PreviousRuntime.Version
    }
    if (Test-Path $RollbackExecutable) {
        $RollbackProcess = Start-Process $RollbackExecutable -ArgumentList @("-start", "-port", $Port, "-dashboard=true") -WorkingDirectory $RollbackWorkingDirectory -PassThru
        if (-not [string]::IsNullOrWhiteSpace($RollbackVersion)) {
            Wait-ForHealthyVersion -HealthUrl $HealthUrl -Version $RollbackVersion -Process $RollbackProcess | Out-Null
        }
    }
    throw $Failure
}
finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $StageBinary, $StageShim, $BackupBinary, $BackupShim
}
