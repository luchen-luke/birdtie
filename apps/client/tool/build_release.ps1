param(
    [Parameter(Mandatory = $true)][string]$ConfigFile,
    [ValidateSet('apk', 'appbundle', 'web')][string]$Target = 'apk',
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
$configPath = (Resolve-Path -LiteralPath $ConfigFile -ErrorAction Stop).Path
$config = Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
$environment = [string]$config.BIRDTIE_ENVIRONMENT
$apiBase = [string]$config.BIRDTIE_API_BASE_URL

if ($environment -notin @('staging', 'production')) {
    throw 'Release configuration requires BIRDTIE_ENVIRONMENT=staging or production.'
}
if ([string]::IsNullOrWhiteSpace($apiBase)) {
    throw 'Release configuration requires BIRDTIE_API_BASE_URL.'
}
$uri = $null
if (-not [Uri]::TryCreate($apiBase, [UriKind]::Absolute, [ref]$uri) -or
    $uri.Scheme -ne 'https' -or [string]::IsNullOrWhiteSpace($uri.Host) -or
    $uri.UserInfo -or $uri.Query -or $uri.Fragment -or
    $uri.Host -match '^(localhost|127\.|10\.0\.2\.2$|0\.0\.0\.0$|::1$)' -or
    $uri.Host.EndsWith('.localhost', [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Release API must be a non-loopback HTTPS URL without credentials, query or fragment.'
}
$tokenName = if ($Target -eq 'web') { 'BIRDTIE_MAPBOX_PUBLIC_TOKEN' } else { 'BIRDTIE_MAPBOX_MOBILE_PUBLIC_TOKEN' }
$publicToken = [string]$config.$tokenName
if (-not $publicToken.StartsWith('pk.') -or $publicToken.Length -lt 20) {
    throw "Release configuration requires a $tokenName public token."
}
if ($Target -eq 'web' -and ($config.BIRDTIE_AMAP_WEB_PUBLIC_KEY -or $config.BIRDTIE_AMAP_SERVICE_HOST)) {
    $amapUri = $null
    if (-not $config.BIRDTIE_AMAP_WEB_PUBLIC_KEY -or
        -not [Uri]::TryCreate([string]$config.BIRDTIE_AMAP_SERVICE_HOST, [UriKind]::Absolute, [ref]$amapUri) -or
        $amapUri.Scheme -ne 'https' -or
        $amapUri.Host -match '^(localhost|127\.|10\.0\.2\.2$|0\.0\.0\.0$|::1$)') {
        throw 'AMap Web key and remote HTTPS service host must be configured together.'
    }
}

Write-Output "Release configuration structure accepted: $environment / $Target / $($uri.Host). Provider access is not verified."
if ($CheckOnly) { return }

$clientDir = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $clientDir
try {
    & flutter build $Target '--release' "--dart-define-from-file=$configPath" '--dart-define=BIRDTIE_RELEASE_CONFIG_VALID=true'
    if ($LASTEXITCODE -ne 0) { throw "Flutter release build failed with exit code $LASTEXITCODE." }
} finally {
    Pop-Location
}
