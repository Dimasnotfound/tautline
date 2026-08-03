param(
    [string]$OldRoot = "D:\PRIMACODES (ME)\chatgpt-mcp",
    [string]$Port = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $Root "bin\tautline.exe"
$StartScript = Join-Path $PSScriptRoot "start.ps1"
$EnvPath = Join-Path $Root ".env"
$ExpectedVersion = "2.5.1"

if (-not (Test-Path $Binary)) {
    throw "The new Tautline executable was not found at $Binary. Run scripts\build.ps1 first."
}

if (-not $Port -and (Test-Path $EnvPath)) {
    foreach ($RawLine in [System.IO.File]::ReadAllLines($EnvPath)) {
        $Line = $RawLine.Trim()
        if ($Line -match '^TAUTLINE_PORT\s*=\s*(.+)$') {
            $Port = $Matches[1].Trim().Trim('"').Trim("'")
            break
        }
    }
}
if (-not $Port) {
    $Port = "7688"
}

Write-Host "Stopping the previous Tautline workspace..." -ForegroundColor Yellow
$OldBinary = Join-Path $OldRoot "versions\v2.4.0\bin\tautline.exe"
if (Test-Path $OldBinary) {
    Push-Location $OldRoot
    try {
        & $OldBinary -stop -port $Port 2>$null | Out-Null
    }
    catch {
        # A forced fallback below handles stale PID state or the Windows stop exit code.
    }
    finally {
        Pop-Location
    }
}

Start-Sleep -Seconds 2
$OldRootLower = $OldRoot.ToLowerInvariant()
$AllowedNames = @("tautline.exe", "lightpanda-shim.exe", "cloudflared.exe", "wsl.exe", "cmd.exe")
$OldProcesses = @(Get-CimInstance Win32_Process | Where-Object {
    $NameMatches = $AllowedNames -contains $_.Name.ToLowerInvariant()
    $ExecutableMatches = $_.ExecutablePath -and $_.ExecutablePath.ToLowerInvariant().StartsWith($OldRootLower)
    $CommandMatches = $_.CommandLine -and $_.CommandLine.ToLowerInvariant().Contains($OldRootLower)
    $NameMatches -and ($ExecutableMatches -or $CommandMatches)
})

foreach ($Process in $OldProcesses) {
    Stop-Process -Id $Process.ProcessId -Force -ErrorAction SilentlyContinue
}

$HealthUrl = "http://127.0.0.1:$Port/healthz"
for ($Attempt = 0; $Attempt -lt 20; $Attempt++) {
    try {
        Invoke-RestMethod -Uri $HealthUrl -TimeoutSec 1 | Out-Null
        Start-Sleep -Milliseconds 500
    }
    catch {
        break
    }
}

Write-Host "Starting Tautline v$ExpectedVersion from $Root..." -ForegroundColor Cyan
$Arguments = "-NoLogo -NoProfile -ExecutionPolicy Bypass -NoExit -File `"$StartScript`" -Port `"$Port`""
$NewProcess = Start-Process powershell.exe -ArgumentList $Arguments -WorkingDirectory $Root -PassThru

$Health = $null
for ($Attempt = 0; $Attempt -lt 60; $Attempt++) {
    if ($NewProcess.HasExited) {
        throw "The new Tautline process exited before the health check succeeded."
    }
    try {
        $Health = Invoke-RestMethod -Uri $HealthUrl -TimeoutSec 2
        if ($Health.service -eq "Tautline" -and $Health.version -eq $ExpectedVersion) {
            break
        }
    }
    catch {
        Start-Sleep -Seconds 1
    }
}

if (-not $Health -or $Health.service -ne "Tautline" -or $Health.version -ne $ExpectedVersion) {
    throw "The new Tautline instance did not become healthy at $HealthUrl."
}

Write-Host "Migration handoff completed." -ForegroundColor Green
Write-Host "Active version : $($Health.version)" -ForegroundColor Green
Write-Host "Active folder  : $Root" -ForegroundColor Green
Write-Host "Dashboard      : http://127.0.0.1:$Port/" -ForegroundColor Green
