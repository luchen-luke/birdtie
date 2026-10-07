param([string]$ConfigPath = (Join-Path $PSScriptRoot '..\apps\client\.env.maps.mobile.local.json'))

$ErrorActionPreference = 'Stop'
$resolved = Resolve-Path -LiteralPath $ConfigPath -ErrorAction Stop
$config = Get-Content -LiteralPath $resolved -Raw | ConvertFrom-Json
$token = $config.BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN
if (-not $token -or $token -notmatch '^pk\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$') {
    throw 'Ignored mobile Mapbox configuration is missing a public token.'
}

function Probe([string]$label,[string]$path) {
    $url = 'https://api.mapbox.com' + $path +
        $(if ($path.Contains('?')) { '&' } else { '?' }) +
        'access_token=' + [Uri]::EscapeDataString($token)
    try {
        $response = Invoke-WebRequest -Uri $url -SkipHttpErrorCheck -TimeoutSec 12
        Write-Host "$label HTTP $($response.StatusCode)"
        return [int]$response.StatusCode
    } catch {
        Write-Host "$label network error"
        return 0
    }
}

$validation = Probe 'Token validation' '/tokens/v2'
$style = Probe 'Birdtie style' '/styles/v1/lookluo/cmth7kwad001p01ssc9kw7kco'
$vector = Probe 'Public vector tile' '/v4/mapbox.mapbox-streets-v8/0/0/0.vector.pbf'
if ($validation -ne 200 -or $style -ne 200 -or $vector -ne 200) {
    Write-Output 'Mapbox native map access is not ready. Inspect the Mapbox account and token restrictions.'
    exit 1
}
Write-Output 'Mapbox token, Birdtie style and public vector tile are reachable.'
