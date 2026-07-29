param(
    [string]$Port = "",
    [ValidateSet("", "quick", "named")]
    [string]$Tunnel = "",
    [switch]$NoDashboard
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$EnvPath = Join-Path $Root ".env"
$Binary = Join-Path $Root "bin/tautline.exe"

function Import-DotEnv {
    param([string]$Path)

    if (-not (Test-Path $Path)) {
        return
    }

    foreach ($RawLine in [System.IO.File]::ReadAllLines($Path)) {
        $Line = $RawLine.Trim()
        if (-not $Line -or $Line.StartsWith("#")) {
            continue
        }

        $Separator = $Line.IndexOf("=")
        if ($Separator -lt 1) {
            throw "Invalid .env line: $RawLine"
        }

        $Name = $Line.Substring(0, $Separator).Trim()
        $Value = $Line.Substring($Separator + 1).Trim()
        if (($Value.StartsWith([char]34) -and $Value.EndsWith([char]34)) -or ($Value.StartsWith("'") -and $Value.EndsWith("'"))) {
            $Value = $Value.Substring(1, $Value.Length - 2)
        }
        [Environment]::SetEnvironmentVariable($Name, $Value, "Process")
    }
}

Import-DotEnv -Path $EnvPath

if (-not (Test-Path $Binary)) {
    & (Join-Path $PSScriptRoot "build.ps1")
    if ($LASTEXITCODE -ne 0) {
        throw "Tautline build failed."
    }
}

$ResolvedPort = if ($Port) { $Port } elseif ($env:TAUTLINE_PORT) { $env:TAUTLINE_PORT } else { "7688" }
$Arguments = @("-start", "-port", $ResolvedPort, "-dashboard=$(!$NoDashboard.IsPresent)")
if ($Tunnel) {
    $Arguments += "-tunnel=$Tunnel"
}

Set-Location $Root
& $Binary @Arguments
exit $LASTEXITCODE
