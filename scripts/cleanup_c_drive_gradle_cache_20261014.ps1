[CmdletBinding()]
param([switch]$Execute)

$ErrorActionPreference = 'Stop'
# Windows PowerShell 5.1 cannot reliably handle the long cache paths. Forward
# the same explicit mode to the PowerShell 7 runtime already on this machine.
if ($PSVersionTable.PSVersion.Major -lt 7) {
    $cachePwshCommand = Get-Command pwsh.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $cachePwshPath = if ($cachePwshCommand) { $cachePwshCommand.Source } else {
        Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\native\powershell\pwsh.exe'
    }
    if (-not (Test-Path -LiteralPath $cachePwshPath -PathType Leaf)) {
        throw 'PowerShell 7 is required and was not found. Nothing was deleted.'
    }
    $cacheLaunchArguments = @('-NoProfile', '-File', $PSCommandPath)
    if ($Execute.IsPresent) { $cacheLaunchArguments += '-Execute' }
    Write-Output 'Using installed PowerShell 7 for long cache paths.'
    & $cachePwshPath @cacheLaunchArguments
    if ($LASTEXITCODE -ne 0) { throw ('Cache cleanup returned exit code ' + $LASTEXITCODE) }
    return
}

$cacheRecords = 'D:\Project\birdtie\docs\reports\c-drive-gradle-cleanup-20261014'
$cacheAllowedRoots = @('C:\bt-g07', 'C:\Users\chens\.gradle', 'C:\Users\chens\.gradle-birdtie-build')
$cacheParsedPlan = Get-Content -LiteralPath (Join-Path $cacheRecords 'plan.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$cachePlan = @($cacheParsedPlan)
$cachePreflight = Get-Content -LiteralPath (Join-Path $cacheRecords 'preflight.json') -Raw -Encoding UTF8 | ConvertFrom-Json
if ($cachePlan.Count -ne 33 -or $cachePreflight.Targets -ne 33) {
    throw 'The reviewed cache plan has changed. Nothing was deleted.'
}
if (-not ('CDriveCleanupSafety' -as [type])) {
    Add-Type -Path (Join-Path $cacheRecords 'cleanup-safety.cs')
}

$cacheCompilerProcesses = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('java.exe', 'dart.exe', 'gradle.exe') })
if ($cacheCompilerProcesses.Count -gt 0) {
    throw 'A Java/Flutter/Gradle process is running. Close the build session before clearing its caches.'
}

# Keep downloaded dependencies, Gradle distributions, platform support and
# original logs. Snapshot their file counts and sizes without reading secrets.
$cacheRetained = [System.Collections.Generic.List[object]]::new()
foreach ($cacheRoot in $cacheAllowedRoots) {
    foreach ($cacheSuffix in @('caches\modules-2', 'wrapper', 'native', 'android', 'notifications', 'daemon', 'kotlin-profile')) {
        $cacheRetainedPath = Join-Path $cacheRoot $cacheSuffix
        if (Test-Path -LiteralPath $cacheRetainedPath) {
            $cacheRetained.Add([CDriveCleanupSafety]::Snapshot($cacheRoot, $cacheRetainedPath))
        }
    }
}
if (-not (Test-Path -LiteralPath 'C:\Users\chens\.gradle\caches\jars-9' -PathType Container)) {
    throw 'Missing jars-9 directory referenced by the retained project junction.'
}
$cacheJunctionTargets = @(
    'D:\Project\birdtie\apps\client\.gradle-user-home\repair\caches\jars-9',
    'D:\Project\birdtie\apps\client\.gradle-user-home\repair\caches\modules-2',
    'D:\Project\birdtie\apps\client\.gradle-user-home\repair\wrapper'
)
foreach ($cacheJunction in $cacheJunctionTargets) {
    if (-not (Test-Path -LiteralPath $cacheJunction -PathType Container)) {
        throw ('A retained project cache link is unresolved: ' + $cacheJunction)
    }
}

$cacheReady = [System.Collections.Generic.List[object]]::new()
foreach ($cacheTarget in $cachePlan) {
    $cacheRoot = [IO.Path]::GetFullPath($cacheTarget.Root).TrimEnd('\')
    $cachePath = [IO.Path]::GetFullPath($cacheTarget.Path)
    if ($cacheRoot -notin $cacheAllowedRoots) { throw 'A target has an unapproved root.' }
    $cacheAllowedPaths = @('caches\9.1.0', 'caches\journal-1', '.tmp') | ForEach-Object { Join-Path $cacheRoot $_ }
    if ($cacheRoot -ne 'C:\Users\chens\.gradle') { $cacheAllowedPaths += Join-Path $cacheRoot 'caches\jars-9' }
    $cacheIsSharedJarChild = $cacheRoot -eq 'C:\Users\chens\.gradle' -and
        [IO.Path]::GetDirectoryName($cachePath) -eq 'C:\Users\chens\.gradle\caches\jars-9'
    if ($cachePath -notin $cacheAllowedPaths -and -not $cacheIsSharedJarChild) {
        throw ('Target is outside the reviewed cache categories: ' + $cachePath)
    }
    if (-not (Test-Path -LiteralPath $cachePath)) {
        Write-Output ('Already absent: ' + $cachePath)
        continue
    }
    $cacheReady.Add([CDriveCleanupSafety]::Inspect($cacheRoot, $cachePath))
}
$cacheBytes = ($cacheReady | Measure-Object Bytes -Sum).Sum
Write-Output ('Verified targets: ' + $cacheReady.Count + '; removable GiB: ' + [math]::Round($cacheBytes / 1GB, 3))
if (-not $Execute) {
    Write-Output 'DRY RUN ONLY. No files deleted. Use -Execute to clear the reviewed compiler caches.'
    return
}

$cacheGeneratedReports = @(
    'docs/reports/c-drive-gradle-cleanup-20261014/execution-results.json',
    'docs/reports/c-drive-gradle-cleanup-20261014/execution-summary.json'
)
$cacheRepositories = @('D:\Project\birdtie', 'D:\Program\Civu')
function Get-CacheProjectState([string]$Repository) {
    $cacheStateRows = @(git -c core.quotepath=false -C $Repository status --porcelain=v1 -uall |
        Where-Object { $Repository -ne 'D:\Project\birdtie' -or $_.Substring(3) -notin $cacheGeneratedReports })
    if ($LASTEXITCODE -ne 0) { throw ('Cannot inspect project Git state: ' + $Repository) }
    $cacheStateHasher = [Security.Cryptography.SHA256]::Create()
    try {
        $cacheStateDigest = [BitConverter]::ToString($cacheStateHasher.ComputeHash([Text.Encoding]::UTF8.GetBytes(($cacheStateRows -join "`n")))).Replace('-', '').ToLowerInvariant()
    } finally { $cacheStateHasher.Dispose() }
    return [PSCustomObject]@{Root=$Repository;Entries=$cacheStateRows.Count;Sha256=$cacheStateDigest}
}
$cacheProjectBefore = @($cacheRepositories | ForEach-Object { Get-CacheProjectState $_ })
$cacheRequiredProjectPaths = @(
    'C:\Users\chens\.civu\signing',
    'D:\Project\birdtie\.git',
    'D:\Program\Civu\.git',
    'D:\Project\birdtie\automation\codex_task_queue.json',
    'D:\Project\birdtie\work\remaining-execution-20261014\apk-build-copy-bc4\birdtie-batch-c.apk',
    'D:\Project\birdtie\work\remaining-execution-20261014\api-build-entity-share-c02\artifact\api.exe',
    'D:\Project\birdtie\work\remaining-execution-20261014\web-build-copy-bc4\artifact\main.dart.js'
)
foreach ($cacheRequiredPath in $cacheRequiredProjectPaths) {
    if (-not (Test-Path -LiteralPath $cacheRequiredPath)) { throw ('Missing protected project path: ' + $cacheRequiredPath) }
}
$cacheStarted = [DateTime]::UtcNow.ToString('o')
$cacheFreeBefore = (Get-PSDrive C).Free
$cacheResults = [System.Collections.Generic.List[object]]::new()
foreach ($cacheTarget in $cacheReady) {
    $cacheResult = [PSCustomObject]@{Path=$cacheTarget.Path;ExpectedBytes=$cacheTarget.Bytes;ExpectedFiles=$cacheTarget.Files;Status='PENDING';Error=$null}
    try {
        if (@(Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('java.exe', 'dart.exe', 'gradle.exe') }).Count -gt 0) {
            throw 'A compiler started during cleanup. This target was left untouched.'
        }
        [void][CDriveCleanupSafety]::Inspect($cacheTarget.Root, $cacheTarget.Path)
        if ((Get-Item -LiteralPath $cacheTarget.Path -Force).PSIsContainer) {
            Remove-Item -LiteralPath $cacheTarget.Path -Recurse -Force -ErrorAction Stop
        } else {
            Remove-Item -LiteralPath $cacheTarget.Path -Force -ErrorAction Stop
        }
        if (Test-Path -LiteralPath $cacheTarget.Path) { throw 'Target remains after removal.' }
        $cacheResult.Status = 'REMOVED'
    } catch {
        $cacheResult.Status = 'FAILED_OR_PARTIAL'
        $cacheResult.Error = $_.Exception.Message
    }
    $cacheResults.Add($cacheResult)
    Write-Output ('Completed targets: ' + $cacheResults.Count + '/' + $cacheReady.Count + '; ' + $cacheResult.Status)
    ConvertTo-Json -InputObject $cacheResults.ToArray() -Depth 5 | Set-Content -LiteralPath (Join-Path $cacheRecords 'execution-results.json') -Encoding UTF8
}

$cacheRetainedUnchanged = $true
foreach ($cacheItem in $cacheRetained) {
    $cacheAfter = [CDriveCleanupSafety]::Snapshot($cacheItem.Root, $cacheItem.Path)
    if ($cacheAfter.Files -ne $cacheItem.Files -or $cacheAfter.Bytes -ne $cacheItem.Bytes) { $cacheRetainedUnchanged = $false }
}
$cacheLinksResolve = @($cacheJunctionTargets | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Container) }).Count -eq 0
$cacheProjectPathsPresent = @($cacheRequiredProjectPaths | Where-Object { -not (Test-Path -LiteralPath $_) }).Count -eq 0
$cacheProjectAfter = @($cacheRepositories | ForEach-Object { Get-CacheProjectState $_ })
$cacheGitUnchanged = $true
for ($cacheIndex=0; $cacheIndex -lt $cacheProjectBefore.Count; $cacheIndex++) {
    if ($cacheProjectBefore[$cacheIndex].Sha256 -ne $cacheProjectAfter[$cacheIndex].Sha256) { $cacheGitUnchanged = $false }
}
$cacheRemoved = @($cacheResults | Where-Object Status -eq 'REMOVED')
$cacheSummary = [PSCustomObject]@{
    StartedUtc = $cacheStarted
    CompletedUtc = [DateTime]::UtcNow.ToString('o')
    RemovedTargets = $cacheRemoved.Count
    FailedOrPartialTargets = @($cacheResults | Where-Object Status -ne 'REMOVED').Count
    RemovedBytesForFullyRemovedTargets = ($cacheRemoved | Measure-Object ExpectedBytes -Sum).Sum
    RemovedGiB = [math]::Round(($cacheRemoved | Measure-Object ExpectedBytes -Sum).Sum / 1GB, 3)
    ProjectGitStateUnchangedExcludingCleanupReports = $cacheGitUnchanged
    ProjectGitStateBefore = $cacheProjectBefore
    ProjectGitStateAfter = $cacheProjectAfter
    RetainedDependencyDistributionAndLogSizesUnchanged = $cacheRetainedUnchanged
    RetainedProjectCacheLinksResolve = $cacheLinksResolve
    SigningAndProjectArtifactsPresent = $cacheProjectPathsPresent
    DriveCFreeBefore = $cacheFreeBefore
    DriveCFreeAfter = (Get-PSDrive C).Free
    Scope = 'Only reviewed C-drive Gradle compiler caches; dependencies, Gradle distributions, project source, signing, configuration, logs and current delivery artifacts preserved.'
}
$cacheSummary | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $cacheRecords 'execution-summary.json') -Encoding UTF8
$cacheSummary | ConvertTo-Json -Depth 6
if (-not $cacheGitUnchanged -or -not $cacheRetainedUnchanged -or -not $cacheLinksResolve -or -not $cacheProjectPathsPresent -or $cacheSummary.FailedOrPartialTargets -gt 0) {
    throw 'Cleanup checks found a failure or concurrent changes. Inspect the execution reports.'
}
