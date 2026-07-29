$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$buf = Join-Path $projectRoot ".tools\bin\buf.exe"
$bufCache = Join-Path $projectRoot ".tools\cache\buf"

if (-not (Test-Path -LiteralPath $buf)) {
    throw "buf is not installed. Run .\scripts\install-tools.ps1 first."
}

New-Item -ItemType Directory -Force -Path $bufCache | Out-Null
$env:BUF_CACHE_DIR = $bufCache

function Invoke-Buf {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    & $buf @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "buf $($Arguments -join ' ') failed (exit code $LASTEXITCODE)."
    }
}

Push-Location $projectRoot
try {
    Invoke-Buf @("lint")
    Invoke-Buf @("generate")
}
finally {
    Pop-Location
}
