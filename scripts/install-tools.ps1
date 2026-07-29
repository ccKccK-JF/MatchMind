param(
    [string]$Proxy = ""
)

$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$toolBin = Join-Path $projectRoot ".tools\bin"
$go = "C:\Program Files\Go\bin\go.exe"

if (-not (Test-Path -LiteralPath $go)) {
    throw "Go was not found at $go. Install Go or update this script."
}

New-Item -ItemType Directory -Force -Path $toolBin | Out-Null
$env:GOBIN = $toolBin

if ($Proxy -ne "") {
    $env:HTTP_PROXY = $Proxy
    $env:HTTPS_PROXY = $Proxy
}

function Install-GoTool {
    param([Parameter(Mandatory = $true)][string]$Package)

    & $go install $Package
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to install $Package (exit code $LASTEXITCODE)."
    }
}

Install-GoTool "github.com/bufbuild/buf/cmd/buf@v1.69.0"
Install-GoTool "google.golang.org/protobuf/cmd/protoc-gen-go@latest"
Install-GoTool "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"

Write-Host "Protobuf tools installed in $toolBin"
