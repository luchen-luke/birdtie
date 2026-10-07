[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = $env:CIVU_MEDIA_DIR
$manifest = $env:CIVU_MEDIA_MANIFEST
if ([string]::IsNullOrWhiteSpace($root)) { throw 'Set CIVU_MEDIA_DIR to the verified media directory.' }
if ([string]::IsNullOrWhiteSpace($manifest)) { throw 'Set CIVU_MEDIA_MANIFEST to a new protected output file outside the source tree.' }
$rootInfo = Get-Item -LiteralPath $root -ErrorAction Stop
if (-not $rootInfo.PSIsContainer) { throw 'CIVU_MEDIA_DIR is not a directory.' }
if (Test-Path -LiteralPath $manifest) { throw 'Manifest already exists; refusing to overwrite.' }
$rootFull = [IO.Path]::GetFullPath($rootInfo.FullName).TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar

$files = @()
$total = [int64]0
Get-ChildItem -LiteralPath $rootInfo.FullName -File -Recurse -Force | ForEach-Object {
    if ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) { return }
    $full = [IO.Path]::GetFullPath($_.FullName)
    if (-not $full.StartsWith($rootFull, [StringComparison]::OrdinalIgnoreCase)) { throw 'A media path escapes the source directory.' }
    $hash = (Get-FileHash -LiteralPath $full -Algorithm SHA256).Hash.ToLowerInvariant()
    $relative = $full.Substring($rootFull.Length).Replace('\', '/')
    $files += [ordered]@{ relative_path = $relative; bytes = $_.Length; sha256 = $hash }
    $total += $_.Length
}
$document = [ordered]@{
    created_at_utc = [DateTime]::UtcNow.ToString('o')
    source_root_label = 'verified media directory'
    file_count = $files.Count
    total_bytes = $total
    files = $files
}
$document | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifest -Encoding utf8NoBOM
Write-Output "Inventory only: files=$($files.Count) bytes=$total. No media copied or backed up."
