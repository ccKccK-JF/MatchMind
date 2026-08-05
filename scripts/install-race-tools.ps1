param(
    [string]$Proxy = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolsRoot = Join-Path $projectRoot ".tools"
$archivePath = Join-Path $toolsRoot "winlibs-gcc-16.1.0-ucrt.zip"
$destination = Join-Path $toolsRoot "winlibs-gcc-16.1.0-ucrt"
$gcc = Join-Path $destination "mingw64\bin\gcc.exe"
$downloadUrl = "https://github.com/brechtsanders/winlibs_mingw/releases/download/16.1.0posix-14.0.0-ucrt-r4/winlibs-x86_64-posix-seh-gcc-16.1.0-mingw-w64ucrt-14.0.0-r4.zip"
$expectedSha256 = "c406a22f8cac82559a3a1d96b62ff603f666499fb5ff4784e87b4eb6fa37dede"

if (Test-Path -LiteralPath $gcc) {
    Write-Host "Race detector compiler already installed at $gcc"
    exit 0
}

New-Item -ItemType Directory -Force -Path $toolsRoot, $destination | Out-Null
if (-not (Test-Path -LiteralPath $archivePath)) {
    Write-Host "Downloading portable MinGW-w64 GCC..."
    $previousProgressPreference = $ProgressPreference
    $ProgressPreference = "SilentlyContinue"
    try {
        if ([string]::IsNullOrWhiteSpace($Proxy)) {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath
        } else {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath -Proxy $Proxy
        }
    } finally {
        $ProgressPreference = $previousProgressPreference
    }
}

$actualSha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash.ToLowerInvariant()
if ($actualSha256 -ne $expectedSha256) {
    throw "WinLibs checksum mismatch: got $actualSha256"
}

Write-Host "Extracting portable MinGW-w64 GCC..."
Expand-Archive -LiteralPath $archivePath -DestinationPath $destination -Force
if (-not (Test-Path -LiteralPath $gcc)) {
    throw "GCC was not found after extraction: $gcc"
}

Write-Host "Race detector compiler installed at $gcc"
