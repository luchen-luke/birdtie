param(
    [string]$ApiBase = 'http://127.0.0.1:3694',
    [string]$OrganizationID = 'a0e9d92e-c3f3-4d81-ad0e-eda0d198b275'
)

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1', 'localhost') -or
    $OrganizationID -notmatch '^[0-9a-f-]{36}$') {
    throw 'Synthetic organization verification requires a loopback API and UUID.'
}
$ApiBase = $ApiBase.TrimEnd('/')
$apiDir = Join-Path $PSScriptRoot '..\apps\api'
function Sql([string]$statement) {
    Push-Location $apiDir
    try {
        $out = & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -Atc $statement
        if ($LASTEXITCODE -ne 0) { throw 'Local SQL verification failed.' }
        return $out
    } finally { Pop-Location }
}
function PublicGet([int]$expected) {
    $response = Invoke-WebRequest -Uri "$ApiBase/v1/organizations/$OrganizationID" -SkipHttpErrorCheck
    if ($response.StatusCode -ne $expected) {
        throw "Public organization returned HTTP $($response.StatusCode), expected $expected."
    }
    if ($expected -eq 200) { return ($response.Content | ConvertFrom-Json).data }
    return $null
}

$baseline = (Sql "SELECT name || '|' || verification_status || '|' || visibility || '|' || description || '|' || official_links::text FROM organizations WHERE id='$OrganizationID';" | Select-Object -First 1)
if ($baseline -ne 'Birdtie 闭环验收组织（本地测试）|unverified|public||[]') {
    throw 'Expected untouched synthetic organization profile; refusing to change other data.'
}
try {
    $profile = PublicGet 200
    if ($profile.verificationStatus -ne 'unverified' -or
        @($profile.upcomingActivities).Count -lt 1 -or
        @($profile.upcomingActivities | Where-Object { $_.organizationId -eq $OrganizationID }).Count -lt 1) {
        throw 'Unverified status or upcoming organization activity is incorrect.'
    }
    Write-Output '[PASS] Public unverified organization and upcoming Activity ownership'

    Sql "UPDATE organizations SET verification_status='verified', description='本地测试组织简介', official_links=jsonb_build_array('https://example.org/birdtie-test') WHERE id='$OrganizationID';" | Out-Null
    $profile = PublicGet 200
    if ($profile.verificationStatus -ne 'verified' -or
        $profile.description -ne '本地测试组织简介' -or
        @($profile.officialLinks).Count -ne 1 -or
        $profile.officialLinks[0] -ne 'https://example.org/birdtie-test') {
        throw 'Verified status, description or HTTPS link was not returned.'
    }
    Write-Output '[PASS] Stored verification state, description and organization-provided link'

    Sql "UPDATE organizations SET visibility='private' WHERE id='$OrganizationID';" | Out-Null
    PublicGet 404 | Out-Null
    Write-Output '[PASS] Private organization is not exposed by the public route'
} finally {
    Sql "UPDATE organizations SET verification_status='unverified', visibility='public', description='', official_links='[]'::jsonb WHERE id='$OrganizationID' AND name='Birdtie 闭环验收组织（本地测试）';" | Out-Null
}
