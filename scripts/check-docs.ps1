$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$requiredDirectories = @(
    "docs\product",
    "docs\design",
    "docs\guides",
    "docs\quality"
)

foreach ($relativeDirectory in $requiredDirectories) {
    $directory = Join-Path $repoRoot $relativeDirectory
    if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
        throw "Missing documentation directory: $relativeDirectory"
    }
}

$markdownFiles = @(
    Get-Item -LiteralPath (Join-Path $repoRoot "README.md")
    Get-ChildItem -LiteralPath (Join-Path $repoRoot "api") -Recurse -File -Filter "*.md"
    Get-ChildItem -LiteralPath (Join-Path $repoRoot "pkg") -Recurse -File -Filter "*.md"
    Get-ChildItem -LiteralPath (Join-Path $repoRoot "docs") -Recurse -File -Filter "*.md"
)

$errors = [System.Collections.Generic.List[string]]::new()
foreach ($file in $markdownFiles) {
    $relativePath = $file.FullName.Substring($repoRoot.Length + 1).Replace("\", "/")
    if ($relativePath -notmatch "^[\x00-\x7F]+$") {
        $errors.Add("Documentation path must use English ASCII: $relativePath")
    }

    $content = Get-Content -LiteralPath $file.FullName -Raw -Encoding UTF8
    if ($content -notmatch "[\u4e00-\u9fff]") {
        $errors.Add("Documentation has no Chinese prose: $relativePath")
    }

    foreach ($match in [regex]::Matches($content, "\]\(([^)#]+)(?:#[^)]+)?\)")) {
        $target = $match.Groups[1].Value
        if ($target -match "^(https?://|mailto:)") {
            continue
        }
        $decodedTarget = [Uri]::UnescapeDataString($target)
        $resolved = [System.IO.Path]::GetFullPath((Join-Path $file.DirectoryName $decodedTarget))
        if (-not (Test-Path -LiteralPath $resolved)) {
            $errors.Add("Documentation link does not exist: $relativePath -> $target")
        }
    }
}

if ($errors.Count -gt 0) {
    $errors | ForEach-Object { Write-Error $_ }
    throw "Documentation check failed with $($errors.Count) issue(s)"
}

Write-Host "Documentation check passed: $($markdownFiles.Count) Markdown files have valid paths, Chinese prose, and links."
