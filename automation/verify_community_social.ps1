param([string]$ApiBase = 'http://127.0.0.1:3696')

$ErrorActionPreference = 'Stop'
$uri = [Uri]$ApiBase
if ($uri.Scheme -ne 'http' -or $uri.Host -notin @('127.0.0.1', 'localhost')) {
    throw 'This synthetic check only runs against a loopback development API.'
}
$ApiBase = $ApiBase.TrimEnd('/')

function Request([string]$method, [string]$path, [object]$body, [string]$token, [int[]]$expected) {
    $args = @{ Uri = "$ApiBase$path"; Method = $method; SkipHttpErrorCheck = $true }
    if ($null -ne $body) {
        $args.ContentType = 'application/json'
        $args.Body = ConvertTo-Json -InputObject $body -Depth 8 -Compress
    }
    if ($token) { $args.Headers = @{ Authorization = "Bearer $token" } }
    $response = Invoke-WebRequest @args
    if ($response.StatusCode -notin $expected) {
        $code = ''
        try { $code = ($response.Content | ConvertFrom-Json).error.code } catch {}
        throw "$method $path returned HTTP $($response.StatusCode) $code"
    }
    if (-not $response.Content) { return $null }
    return $response.Content | ConvertFrom-Json
}

function DevToken([string]$phone) {
    Request 'POST' '/v1/auth/dev-phone/code' @{phone=$phone} '' @(200) | Out-Null
    return (Request 'POST' '/v1/auth/dev-phone/verify' @{phone=$phone;code='123456'} '' @(200)).data.accessToken
}

if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Local development phone authentication is disabled.'
}
Request 'GET' '/readyz' $null '' @(200) | Out-Null
$owner = DevToken '+999000000071'
$student = DevToken '+999000000072'
$personID = 'b1700000-0000-4000-8000-000000000010'
$studentID = 'b1700000-0000-4000-8000-000000000011'
$publicID = 'b1700000-0000-4000-8000-000000000030'
$privateID = 'b1700000-0000-4000-8000-000000000031'
$hiddenID = 'b1700000-0000-4000-8000-000000000032'
$organizationID = 'b1700000-0000-4000-8000-000000000013'

$discover = @((Request 'GET' '/v1/communities?cityId=aberdeen-gb' $null $student @(200)).data | ForEach-Object id)
if ($publicID -notin $discover -or $privateID -notin $discover -or $hiddenID -in $discover) {
    throw 'Public/private/hidden Community discovery boundary failed.'
}
$mine = @((Request 'GET' '/v1/me/social-communities' $null $student @(200)).data | ForEach-Object id)
if ($privateID -notin $mine -or $hiddenID -notin $mine) {
    throw 'Pending request or hidden invitation missing from My Communities.'
}
Write-Output '[PASS] Synthetic public/open, private/request and hidden/invite seed discovery'

$ukNow = [TimeZoneInfo]::ConvertTimeFromUtc([DateTime]::UtcNow, [TimeZoneInfo]::FindSystemTimeZoneById('GMT Standard Time'))
$start = [TimeZoneInfo]::ConvertTimeToUtc($ukNow.Date.AddDays(4).AddHours(18), [TimeZoneInfo]::FindSystemTimeZoneById('GMT Standard Time'))
if ($start -lt [DateTime]::UtcNow.AddHours(24)) { $start = $start.AddDays(7) }
$run = [DateTime]::UtcNow.ToString('yyyyMMddHHmmssfff')
$base = @{
    cityId='aberdeen-gb'; title="Birdtie 合成活动 $run";
    summary='虚构活动，仅供本地开发验收。';
    startsAt=$start.ToString('yyyy-MM-ddTHH:mm:ssZ');
    endsAt=$start.AddHours(2).ToString('yyyy-MM-ddTHH:mm:ssZ');
    timeZone='Europe/London'; categoryCode='social';
    capacity=12; priceMinor=0; languageCode='zh-CN'; visibility='public'
}
$created = @()
try {
    function CreateActivity([string]$kind, [string]$id, [string]$visibility) {
        $input = $base.Clone()
        $input.title = "Birdtie $kind 合成活动 $run"
        $input.visibility = $visibility
        $input.organizer = @{type=$kind;id=$id}
        $activity = (Request 'POST' '/v1/me/activities' $input $owner @(201)).data
        $script:created += $activity.id
        $published = (Request 'POST' "/v1/me/activities/$($activity.id)/publish" $null $owner @(200)).data
        if ($published.organizer.type -ne $kind -or $published.organizer.id -ne $id) {
            throw "$kind organizer did not persist."
        }
        return $published
    }

    $community = CreateActivity 'COMMUNITY' $privateID 'public'
    $members = CreateActivity 'COMMUNITY' $privateID 'organizer_members'
    $person = CreateActivity 'PERSON' $personID 'invite_only'
    $organization = CreateActivity 'ORGANIZATION' $organizationID 'public'
    $publicActivities = @((Request 'GET' '/v1/cities/aberdeen-gb/activities' $null $student @(200)).data | ForEach-Object id)
    if ($community.id -notin $publicActivities -or $organization.id -notin $publicActivities -or
        $members.id -in $publicActivities -or $person.id -in $publicActivities) {
        throw 'Activity public discovery visibility failed.'
    }
    Request 'GET' "/v1/activities/$($community.id)" $null $student @(200) | Out-Null
    Request 'GET' "/v1/activities/$($members.id)" $null $student @(404) | Out-Null
    Request 'POST' "/v1/activities/$($members.id)/participations" $null $student @(404) | Out-Null
    Request 'GET' "/v1/activities/$($members.id)" $null $owner @(200) | Out-Null
    Request 'GET' "/v1/activities/$($person.id)" $null $student @(404) | Out-Null
    Request 'POST' "/v1/me/activities/$($person.id)/invitations" @{userAccountId=$studentID} $owner @(204) | Out-Null
    Request 'GET' "/v1/activities/$($person.id)" $null $student @(200) | Out-Null
    $rsvp = (Request 'POST' "/v1/activities/$($community.id)/participations" $null $student @(201)).data
    if ($rsvp.status -ne 'going') { throw 'Public Community Activity RSVP failed.' }
    $studentCommunity = (Request 'GET' "/v1/communities/$privateID" $null $student @(200)).data
    if ($studentCommunity.myStatus -ne 'pending') {
        throw 'Activity RSVP changed Community membership.'
    }
    Write-Output '[PASS] Person, Community and Organization organizer Activity API flows'
    Write-Output '[PASS] Public outsider RSVP without Community membership; member-only and invite-only ACL'
    Write-Output "[PASS] Synthetic Activity IDs: $($created -join ', ')"
}
finally {
    foreach ($id in $created) {
        try { Request 'POST' "/v1/me/activities/$id/cancel" $null $owner @(200) | Out-Null }
        catch { Write-Warning "Synthetic Activity $id cleanup failed: $_" }
    }
}
