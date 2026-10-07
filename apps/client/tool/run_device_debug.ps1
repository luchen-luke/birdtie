param(
    [string]$DeviceId,
    [ValidateRange(1, 65535)][int]$ApiPort = 3694,
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
$clientDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$tokenFile = Join-Path $clientDir '.env.maps.mobile.local.json'
$apiBase = "http://127.0.0.1:$ApiPort"

if (-not (Test-Path -LiteralPath $tokenFile)) {
    throw "Missing saved mobile Mapbox configuration: $tokenFile"
}
$mapConfig = Get-Content -LiteralPath $tokenFile -Raw | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($mapConfig.BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN)) {
    throw 'Saved mobile Mapbox configuration does not contain BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN.'
}

$attached = @(adb devices | Select-Object -Skip 1 | Where-Object { $_ -match '^\S+\s+device(?:\s|$)' } | ForEach-Object { ($_ -split '\s+')[0] })
if ($LASTEXITCODE -ne 0) { throw 'adb devices failed.' }
if ([string]::IsNullOrWhiteSpace($DeviceId)) {
    if ($attached.Count -ne 1) { throw "Expected one attached Android device; found $($attached.Count). Pass -DeviceId explicitly." }
    $DeviceId = $attached[0]
}
if ($DeviceId -notin $attached) { throw "Device $DeviceId is not attached and authorized." }

try {
    $hostReady = Invoke-RestMethod -Uri "$apiBase/readyz" -TimeoutSec 5
} catch {
    throw "Birdtie API is not ready at $apiBase. Start the local Go API before installing the debug app."
}
if ($hostReady.status -ne 'ready') { throw 'Birdtie API did not report ready status.' }

# Device loopback reaches the host only while this explicit ADB tunnel exists.
& adb -s $DeviceId reverse "tcp:$ApiPort" "tcp:$ApiPort"
if ($LASTEXITCODE -ne 0) { throw 'adb reverse failed.' }
$deviceReady = & adb -s $DeviceId shell curl --max-time 5 --silent --show-error --fail "$apiBase/readyz"
if ($LASTEXITCODE -ne 0 -or ($deviceReady -join '') -notmatch '"status"\s*:\s*"ready"') {
    throw 'The phone could not reach the Birdtie API through adb reverse.'
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
