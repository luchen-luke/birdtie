[CmdletBinding()]
param([switch]$Execute)

# Dry run by default. This file was prepared for manual review after automated
# deletion commands were rejected by the execution approval policy.
$ErrorActionPreference = 'Stop'
# .NET Framework / Windows PowerShell 5.1 cannot reliably inspect and remove
# the long Gradle cache paths in this plan. Use the installed PowerShell 7
# runtime for the same script and preserve the caller's explicit Execute flag.
if ($PSVersionTable.PSVersion.Major -lt 7) {
    $cleanupPwshCommand = Get-Command pwsh.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $cleanupPwshPath = if ($cleanupPwshCommand) { $cleanupPwshCommand.Source } else {
        Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\native\powershell\pwsh.exe'
    }
    if (-not (Test-Path -LiteralPath $cleanupPwshPath -PathType Leaf)) {
        throw 'PowerShell 7 is required for long cache paths and was not found. Nothing was deleted.'
    }
    $cleanupLaunchArguments = @('-NoProfile', '-File', $PSCommandPath)
    if ($Execute.IsPresent) { $cleanupLaunchArguments += '-Execute' }
    Write-Output 'Using installed PowerShell 7 for long cache paths.'
    & $cleanupPwshPath @cleanupLaunchArguments
    if ($LASTEXITCODE -ne 0) { throw ('PowerShell 7 cleanup returned exit code ' + $LASTEXITCODE) }
    return
}
$cleanupRoot = [IO.Path]::GetFullPath('D:\Project\birdtie').TrimEnd('\')
$cleanupRecords = Join-Path $cleanupRoot 'docs\reports\storage-cleanup-20261014'
# Windows PowerShell 5.1 emits a JSON array as one pipeline object. Normalize
# the assigned result separately so both PowerShell 5.1 and 7 read 561 items.
$cleanupParsedPlan = Get-Content -LiteralPath (Join-Path $cleanupRecords 'plan.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$cleanupPlan = @($cleanupParsedPlan)
if (-not ('BirdtieCleanupSafety' -as [type])) {
    Add-Type -Path (Join-Path $cleanupRecords 'cleanup-safety.cs')
}

if ((Get-Item -LiteralPath $cleanupRoot -Force).Attributes -band [IO.FileAttributes]::ReparsePoint) {
    throw 'The official repository root is a reparse point. Nothing was deleted.'
}
if ($cleanupPlan.Count -ne 561) {
    throw 'The reviewed cleanup plan has changed. Nothing was deleted.'
}

$cleanupGitPaths = @(git -c core.quotepath=false -C $cleanupRoot ls-files --cached --others --exclude-standard)
if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect protected Git files.' }
# The script creates/updates only these two report files while it runs. They
# are excluded from source integrity checks; all other repository files stay
# protected, including this script and the reviewed cleanup plan.
$cleanupGeneratedReports = @(
    'docs/reports/storage-cleanup-20261014/manual-execution-results.json',
    'docs/reports/storage-cleanup-20261014/manual-execution-summary.json'
)
$cleanupGitPaths = @($cleanupGitPaths | Where-Object { $_ -notin $cleanupGeneratedReports })
$cleanupProtected = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
foreach ($cleanupRelative in $cleanupGitPaths) { [void]$cleanupProtected.Add($cleanupRelative) }

# Verify the retained APK, API, Web output, screenshots and diagnostics first.
foreach ($cleanupLine in Get-Content -LiteralPath (Join-Path $cleanupRecords 'retained-before.tsv')) {
    if ([string]::IsNullOrWhiteSpace($cleanupLine)) { continue }
    $cleanupParts = $cleanupLine.Split([char]9, 3)
    $cleanupRetainedPath = [IO.Path]::GetFullPath((Join-Path $cleanupRoot $cleanupParts[2]))
    if (-not $cleanupRetainedPath.StartsWith($cleanupRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'A retained file is outside the official repository.'
    }
    if (-not (Test-Path -LiteralPath $cleanupRetainedPath -PathType Leaf)) {
        throw ('Missing retained file; re-audit before cleanup: ' + $cleanupParts[2])
    }
    $cleanupRetainedHash = (Get-FileHash -LiteralPath $cleanupRetainedPath -Algorithm SHA256).Hash
    if ($cleanupRetainedHash -ne $cleanupParts[0]) {
        throw ('Retained delivery/evidence changed; re-audit before cleanup: ' + $cleanupParts[2])
    }
}

$cleanupReady = [System.Collections.Generic.List[object]]::new()
foreach ($cleanupTarget in $cleanupPlan) {
    $cleanupFull = [IO.Path]::GetFullPath($cleanupTarget.Path)
    $cleanupExpected = [IO.Path]::GetFullPath((Join-Path $cleanupRoot $cleanupTarget.RelativePath))
    if ($cleanupFull -ne $cleanupExpected -or
        -not $cleanupFull.StartsWith($cleanupRoot + '\', [StringComparison]::OrdinalIgnoreCase) -or
        $cleanupTarget.RelativePath -match '(^|[\\/])\.git([\\/]|$)' -or
        $cleanupTarget.RelativePath.StartsWith('docs\', [StringComparison]::OrdinalIgnoreCase)) {
        throw ('Unexpected target; nothing was deleted: ' + $cleanupTarget.RelativePath)
    }
    if (-not (Test-Path -LiteralPath $cleanupFull)) {
        Write-Output ('Already absent: ' + $cleanupTarget.RelativePath)
        continue
    }
    # Traverses only normal entries, rejects every internal/ancestor reparse
    # point, and refuses any tracked or untracked Git/source file.
    $cleanupInfo = [BirdtieCleanupSafety]::Inspect($cleanupRoot, $cleanupFull, $cleanupProtected)
    if ($cleanupTarget.Kind -eq 'File' -and $cleanupInfo.Bytes -ne $cleanupTarget.Bytes) {
        throw ('Old binary changed; re-audit before cleanup: ' + $cleanupTarget.RelativePath)
    }
    $cleanupTarget | Add-Member -NotePropertyName CurrentBytes -NotePropertyValue $cleanupInfo.Bytes -Force
    $cleanupTarget | Add-Member -NotePropertyName CurrentFiles -NotePropertyValue $cleanupInfo.Files -Force
    $cleanupReady.Add($cleanupTarget)
}

$cleanupBytes = ($cleanupReady | Measure-Object CurrentBytes -Sum).Sum
Write-Output ('Verified targets: ' + $cleanupReady.Count + '; removable GiB: ' + [math]::Round($cleanupBytes / 1GB, 3))
if (-not $Execute) {
    Write-Output 'DRY RUN ONLY. No files deleted. Review plan.json, then run this script with -Execute.'
    return
}

$cleanupActiveBuilders = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('go.exe', 'dart.exe', 'java.exe') })
if ($cleanupActiveBuilders.Count -gt 0) {
    throw 'A compiler/build process is running. Close it and re-audit before cleanup.'
}

$cleanupSourceHashSource = @'
using System;
using System.IO;
using System.Security.Cryptography;
using System.Text;
public static class BirdtieManualSourceHasher {
    public static string Hash(string root, string[] paths) {
        using (var fileHasher = SHA256.Create())
        using (var manifestHasher = IncrementalHash.CreateHash(HashAlgorithmName.SHA256)) {
            foreach (var relative in paths) {
                var path = Path.GetFullPath(Path.Combine(root, relative));
                if (!path.StartsWith(root + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
                    throw new Exception("Protected path outside repository");
                using (var stream = File.OpenRead(path)) {
                    var digest = BitConverter.ToString(fileHasher.ComputeHash(stream));
                    manifestHasher.AppendData(Encoding.UTF8.GetBytes(relative + "\t" + digest + "\n"));
                }
            }
            return BitConverter.ToString(manifestHasher.GetHashAndReset()).Replace("-", "").ToLowerInvariant();
        }
    }
}
'@
if (-not ('BirdtieManualSourceHasher' -as [type])) {
    Add-Type -TypeDefinition $cleanupSourceHashSource -Language CSharp
}
$cleanupSourceBefore = [BirdtieManualSourceHasher]::Hash($cleanupRoot, $cleanupGitPaths)
$cleanupStatusBefore = @(git -c core.quotepath=false -C $cleanupRoot status --porcelain=v1 -uall | Where-Object { $_.Substring(3) -notin $cleanupGeneratedReports })
$cleanupFreeBefore = (Get-PSDrive -Name D).Free
$cleanupStarted = [DateTime]::UtcNow.ToString('o')
$cleanupResults = [System.Collections.Generic.List[object]]::new()

foreach ($cleanupTarget in $cleanupReady) {
    $cleanupResult = [PSCustomObject]@{
        RelativePath = $cleanupTarget.RelativePath
        ExpectedBytes = $cleanupTarget.CurrentBytes
        ExpectedFiles = $cleanupTarget.CurrentFiles
        Status = 'PENDING'
        Error = $null
    }
    try {
        # Repeat containment and link checks immediately before every removal.
        [void][BirdtieCleanupSafety]::Inspect($cleanupRoot, $cleanupTarget.Path, $cleanupProtected)
        if ($cleanupTarget.Kind -eq 'Directory') {
            Remove-Item -LiteralPath $cleanupTarget.Path -Recurse -Force -ErrorAction Stop
        } else {
            Remove-Item -LiteralPath $cleanupTarget.Path -Force -ErrorAction Stop
        }
        if (Test-Path -LiteralPath $cleanupTarget.Path) { throw 'Target remains after deletion.' }
        $cleanupResult.Status = 'REMOVED'
    } catch {
        $cleanupResult.Status = 'FAILED_OR_PARTIAL'
        $cleanupResult.Error = $_.Exception.Message
    }
    $cleanupResults.Add($cleanupResult)
    if ($cleanupResults.Count % 25 -eq 0 -or $cleanupTarget.CurrentBytes -gt 1GB) {
        Write-Output ('Completed targets: ' + $cleanupResults.Count + '/' + $cleanupReady.Count)
        ConvertTo-Json -InputObject $cleanupResults.ToArray() -Depth 5 | Set-Content -LiteralPath (Join-Path $cleanupRecords 'manual-execution-results.json') -Encoding UTF8
    }
}

$cleanupSourceAfter = [BirdtieManualSourceHasher]::Hash($cleanupRoot, $cleanupGitPaths)
$cleanupStatusAfter = @(git -c core.quotepath=false -C $cleanupRoot status --porcelain=v1 -uall | Where-Object { $_.Substring(3) -notin $cleanupGeneratedReports })
$cleanupRemoved = @($cleanupResults | Where-Object Status -eq 'REMOVED')
$cleanupResultSummary = [PSCustomObject]@{
    Root = $cleanupRoot
    StartedUtc = $cleanupStarted
    CompletedUtc = [DateTime]::UtcNow.ToString('o')
    RemovedTargets = $cleanupRemoved.Count
    FailedOrPartialTargets = @($cleanupResults | Where-Object Status -ne 'REMOVED').Count
    RemovedBytesForFullyRemovedTargets = ($cleanupRemoved | Measure-Object ExpectedBytes -Sum).Sum
    ProtectedSourceHashBefore = $cleanupSourceBefore
    ProtectedSourceHashAfter = $cleanupSourceAfter
    ProtectedSourceContentUnchanged = ($cleanupSourceBefore -eq $cleanupSourceAfter)
    GitStatusUnchangedExcludingCleanupLogs = (($cleanupStatusBefore -join "`n") -ceq ($cleanupStatusAfter -join "`n"))
    ExcludedCleanupLogFiles = $cleanupGeneratedReports
    DriveDFreeBefore = $cleanupFreeBefore
    DriveDFreeAfter = (Get-PSDrive -Name D).Free
    Scope = 'Caches and superseded generated outputs only; no tests, builds or task queue updates.'
}
ConvertTo-Json -InputObject $cleanupResults.ToArray() -Depth 5 | Set-Content -LiteralPath (Join-Path $cleanupRecords 'manual-execution-results.json') -Encoding UTF8
$cleanupResultSummary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $cleanupRecords 'manual-execution-summary.json') -Encoding UTF8
$cleanupResultSummary | ConvertTo-Json -Depth 5
if (-not $cleanupResultSummary.ProtectedSourceContentUnchanged -or -not $cleanupResultSummary.GitStatusUnchangedExcludingCleanupLogs) {
    throw 'Protected source changed during cleanup. Inspect the recorded result before continuing.'
}
if ($cleanupResultSummary.FailedOrPartialTargets -gt 0) {
    throw 'Some targets failed or were partially removed. Inspect manual-execution-results.json.'
}
