param(
    [string]$Port = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Binary = Join-Path $Root "bin/tautline.exe"

if (-not (Test-Path $Binary)) {
    throw "Tautline executable was not found at $Binary."
}

$ResolvedPort = if ($Port) { $Port } elseif ($env:TAUTLINE_PORT) { $env:TAUTLINE_PORT } elseif ($env:PORT) { $env:PORT } else { "7688" }
& $Binary -stop -port $ResolvedPort
exit $LASTEXITCODE
