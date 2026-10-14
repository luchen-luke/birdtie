param(
    [string]$DeviceId,
    [ValidateRange(1, 65535)][int]$ApiPort = 3694,
    [string]$PreviewConfigFile,
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
$clientDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$tokenFile = Join-Path $clientDir '.env.maps.mobile.local.json'
$adbCommand = 'adb'
$serverPort = $ApiPort
$defaultPreviewConfig = Join-Path $clientDir '.env.preview.local.json'
if ([string]::IsNullOrWhiteSpace($PreviewConfigFile) -and
    -not $PSBoundParameters.ContainsKey('ApiPort') -and
    (Test-Path -LiteralPath $defaultPreviewConfig)) {
    $PreviewConfigFile = $defaultPreviewConfig
}
if (-not [string]::IsNullOrWhiteSpace($PreviewConfigFile)) {
    $previewPath = (Resolve-Path -LiteralPath $PreviewConfigFile).Path
    $preview = Get-Content -LiteralPath $previewPath -Raw | ConvertFrom-Json
    if (-not [string]::IsNullOrWhiteSpace($DeviceId) -and $DeviceId -ne $preview.deviceId) {
        throw 'DeviceId differs from the explicitly configured preview device.'
    }
    if ($PSBoundParameters.ContainsKey('ApiPort') -and $ApiPort -ne [int]$preview.devicePort) {
        throw 'ApiPort differs from the configured device port. Update the preview configuration first.'
    }
    $DeviceId = [string]$preview.deviceId
    $ApiPort = [int]$preview.devicePort
    $serverPort = ([Uri]("http://" + $preview.apiAddress)).Port
    $adbCommand = [string]$preview.adbPath
    if (-not [System.IO.Path]::IsPathRooted($adbCommand)) {
        $adbCommand = Join-Path (Resolve-Path (Join-Path $clientDir '..\..')).Path $adbCommand
    }
    if (-not (Test-Path -LiteralPath $adbCommand -PathType Leaf)) { throw 'Configured preview adb executable is missing.' }
}
$apiBase = "http://127.0.0.1:$ApiPort"
$serverBase = "http://127.0.0.1:$serverPort"

if (-not (Test-Path -LiteralPath $tokenFile)) {
    throw "Missing saved mobile Mapbox configuration: $tokenFile"
}
$mapConfig = Get-Content -LiteralPath $tokenFile -Raw | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($mapConfig.BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN)) {
    throw 'Saved mobile Mapbox configuration does not contain BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN.'
}

$attached = @(& $adbCommand devices | Select-Object -Skip 1 | Where-Object { $_ -match '^\S+\s+device(?:\s|$)' } | ForEach-Object { ($_ -split '\s+')[0] })
if ($LASTEXITCODE -ne 0) { throw 'adb devices failed.' }
if ([string]::IsNullOrWhiteSpace($DeviceId)) {
    if ($attached.Count -ne 1) { throw "Expected one attached Android device; found $($attached.Count). Pass -DeviceId explicitly." }
    $DeviceId = $attached[0]
}
if ($DeviceId -notin $attached) { throw "Device $DeviceId is not attached and authorized." }

if (-not [string]::IsNullOrWhiteSpace($PreviewConfigFile)) {
    & (Join-Path $PSScriptRoot 'start_local_preview.ps1') -Action Start -ConfigPath $previewPath
    if ($LASTEXITCODE -ne 0) { throw 'The local preview runtime could not start.' }
}

$hostAvailable = $false
$readyDeadline = [DateTime]::UtcNow.AddSeconds(20)
do {
    try {
        $hostReady = Invoke-RestMethod -Uri "$serverBase/readyz" -TimeoutSec 2
        $hostCities = Invoke-RestMethod -Uri "$serverBase/v1/cities" -TimeoutSec 2
        $hostAvailable = $hostReady.status -eq 'ready' -and $hostCities.PSObject.Properties.Name -contains 'data'
    } catch {
        $hostAvailable = $false
    }
    if (-not $hostAvailable) { Start-Sleep -Milliseconds 500 }
} while (-not $hostAvailable -and [DateTime]::UtcNow -lt $readyDeadline)
if (-not $hostAvailable) {
    throw "Birdtie API or city catalog is unavailable at $serverBase. Start the local preview runtime before installing the debug app."
}

# Device loopback reaches the host only while this explicit ADB tunnel exists.
$mappingPresent = $false
$mappings = & $adbCommand -s $DeviceId reverse --list
if ($LASTEXITCODE -ne 0) { throw 'Could not inspect adb reverse mappings.' }
foreach ($mapping in $mappings) {
    $parts = $mapping.Trim() -split '\s+'
    if ($parts.Count -eq 3 -and $parts[1] -eq "tcp:$ApiPort") {
        if ($parts[2] -ne "tcp:$serverPort") { throw 'An existing adb reverse maps the app port to a different service. It was preserved.' }
        $mappingPresent = $true
    }
}
if (-not $mappingPresent) {
    & $adbCommand -s $DeviceId reverse --no-rebind "tcp:$ApiPort" "tcp:$serverPort"
    if ($LASTEXITCODE -ne 0) { throw 'adb reverse failed or another session claimed the app port.' }
}
$deviceReady = & $adbCommand -s $DeviceId shell curl --max-time 5 --silent --show-error --fail "$apiBase/readyz"
if ($LASTEXITCODE -ne 0 -or ($deviceReady -join '') -notmatch '"status"\s*:\s*"ready"') {
    throw 'The phone could not reach the Birdtie API through adb reverse.'
}
$deviceCities = & $adbCommand -s $DeviceId shell curl --max-time 5 --silent --show-error --fail "$apiBase/v1/cities"
if ($LASTEXITCODE -ne 0 -or ($deviceCities -join '') -notmatch '"data"\s*:\s*\[') {
    throw 'The phone could not read the city catalog through adb reverse.'
}
Write-Output "Birdtie API ready on host and device $DeviceId through adb reverse tcp:$ApiPort."
if ($CheckOnly) { return }

Push-Location $clientDir
try {
    & flutter run -d $DeviceId '--dart-define-from-file=.env.maps.mobile.local.json' '--dart-define=BIRDTIE_ENVIRONMENT=development' "--dart-define=BIRDTIE_API_BASE_URL=$apiBase"
    if ($LASTEXITCODE -ne 0) { throw "flutter run failed with exit code $LASTEXITCODE." }
} finally {
    Pop-Location
}
