$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$projectRoot = Split-Path -Parent $PSScriptRoot
$goExecutable = "C:\Program Files\Go\bin\go.exe"
$compilerBin = Join-Path $projectRoot ".tools\winlibs-gcc-16.1.0-ucrt\mingw64\bin"
$gcc = Join-Path $compilerBin "gcc.exe"

if (-not (Test-Path -LiteralPath $gcc)) {
    throw "Race compiler is missing. Run .\scripts\install-race-tools.ps1 first."
}

$env:PATH = $compilerBin + [IO.Path]::PathSeparator + $env:PATH
$env:CC = $gcc
$env:CGO_ENABLED = "1"
$env:GOCACHE = Join-Path $projectRoot ".cache\go-build-race"

Push-Location $projectRoot
try {
    & $goExecutable test -race -count=1 .\...
    if ($LASTEXITCODE -ne 0) {
        throw "Race detector failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
