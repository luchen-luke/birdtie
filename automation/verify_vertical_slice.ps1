param(
    [string]$ApiBase = 'http://127.0.0.1:3694',
    [string]$AdminPhone = '13800138045',
    [string]$StudentPhone = '13800138046',
    [switch]$KeepTestActivity
)

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1', 'localhost')) {
    throw 'This synthetic E2E check only runs against a loopback development API.'
}
$ApiBase = $ApiBase.TrimEnd('/')

function Request([string]$method, [string]$path, [object]$body, [string]$token, [int[]]$expected) {
    $parameters = @{
        Uri = "$ApiBase$path"
        Method = $method
        SkipHttpErrorCheck = $true
    }
    if ($null -ne $body) {
        $parameters.ContentType = 'application/json'
        $parameters.Body = ConvertTo-Json -InputObject $body -Depth 8 -Compress
    }
    if ($token) { $parameters.Headers = @{ Authorization = "Bearer $token" } }
    $response = Invoke-WebRequest @parameters
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
    $verified = Request 'POST' '/v1/auth/dev-phone/verify' @{phone=$phone;code='123456'} '' @(200)
    return $verified.data.accessToken
}

$status = Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)
if (-not $status.data.enabled) { throw 'Local development phone authentication is disabled.' }
$ready = Request 'GET' '/readyz' $null '' @(200)
if ($ready.status -ne 'ready') { throw 'Development API is not ready.' }

$adminToken = DevToken $AdminPhone
$studentToken = DevToken $StudentPhone
$organizationName = 'Birdtie 闭环验收组织（本地测试）'
$organizations = (Request 'GET' '/v1/me/organizations' $null $adminToken @(200)).data
$organization = $organizations | Where-Object { $_.name -eq $organizationName } | Select-Object -First 1
if (-not $organization) {
    $organization = (Request 'POST' '/v1/me/organizations' @{
        organizationType='student_society';name=$organizationName
    } $adminToken @(201)).data
}
$organizationID = $organization.id
Write-Output "[PASS] Organization admin workspace: $organizationID"

$uk = [TimeZoneInfo]::FindSystemTimeZoneById('GMT Standard Time')
$ukNow = [TimeZoneInfo]::ConvertTimeFromUtc([DateTime]::UtcNow, $uk)
$daysUntilSaturday = (([int][DayOfWeek]::Saturday - [int]$ukNow.DayOfWeek + 7) % 7)
if ($daysUntilSaturday -eq 0 -and $ukNow.TimeOfDay -ge [TimeSpan]::FromHours(11)) {
    $daysUntilSaturday = 7
}
$startUTC = [TimeZoneInfo]::ConvertTimeToUtc($ukNow.Date.AddDays($daysUntilSaturday).AddHours(11), $uk)
$endUTC = $startUTC.AddHours(2)
$title = '闭环验收羽毛球（本地测试）' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
$input = @{
    cityId='aberdeen-gb'
    placeId='b1700000-0000-4000-8000-000000000004'
    title=$title
    summary='仅供 Birdtie 本地端到端验收的虚构活动。'
    description='验证组织发布、学生发现、Agent 结果、报名及我的活动。'
    startsAt=$startUTC.ToString('yyyy-MM-ddTHH:mm:ssZ')
    endsAt=$endUTC.ToString('yyyy-MM-ddTHH:mm:ssZ')
    timeZone='Europe/London'
    categoryCode='badminton'
    capacity=12
    priceMinor=0
    currency=''
    eligibility=''
    languageCode='zh-CN'
    visibility='public'
}
$activityID = $null
try {
    $draft = (Request 'POST' "/v1/me/organizations/$organizationID/activities" $input $adminToken @(201)).data
    $activityID = $draft.id
    if ($draft.publicationStatus -ne 'draft') { throw 'New activity is not a draft.' }
    $published = (Request 'POST' "/v1/me/organizations/$organizationID/activities/$activityID/publish" $null $adminToken @(200)).data
    if ($published.publicationStatus -ne 'published') { throw 'Activity did not publish.' }
    Write-Output "[PASS] Admin draft and publish: $activityID"

    $public = (Request 'GET' '/v1/cities/aberdeen-gb/activities' $null $studentToken @(200)).data
    if ($activityID -notin @($public | ForEach-Object id)) { throw 'Student public discovery omitted the published activity.' }
    $agent = (Request 'POST' '/v1/cities/aberdeen-gb/agent/tasks' @{query='帮我找周末的羽毛球活动'} $studentToken @(200)).data
    if ($activityID -notin @($agent.activities | ForEach-Object id)) { throw 'Agent did not return the published activity.' }
    Write-Output '[PASS] Student discovery and Agent result'

    $detail = (Request 'GET' "/v1/activities/$activityID" $null $studentToken @(200)).data
    if ($detail.id -ne $activityID -or $detail.title -ne $title -or $detail.capacity -ne 12) {
        throw 'Student detail does not match the published activity.'
    }
    Write-Output '[PASS] Public detail and published fields'

    $first = (Request 'POST' "/v1/activities/$activityID/participations" $null $studentToken @(201)).data
    $duplicate = (Request 'POST' "/v1/activities/$activityID/participations" $null $studentToken @(200)).data
    if ($first.status -ne 'going' -or $first.id -ne $duplicate.id) { throw 'RSVP persistence or duplicate protection failed.' }
    $own = (Request 'GET' "/v1/activities/$activityID/participations/me" $null $studentToken @(200)).data
    if ($own.id -ne $first.id) { throw 'RSVP reload did not preserve the same row.' }
    Write-Output '[PASS] RSVP, duplicate protection and reload'

    $plans = (Request 'GET' '/v1/me/participations' $null $studentToken @(200)).data
    $entry = $plans | Where-Object { $_.activityId -eq $activityID } | Select-Object -First 1
    if (-not $entry -or $entry.status -ne 'going' -or $entry.activityStatus -ne 'upcoming') {
        throw 'My Activities did not reflect server RSVP truth.'
    }
    Write-Output '[PASS] My Activities server projection'

    $beforeSaved = (Request 'GET' '/v1/me/saved' $null $studentToken @(200)).data
    if (@($beforeSaved | Where-Object { $_.kind -eq 'activity' -and $_.targetId -eq $activityID }).Count -ne 0) {
        throw 'RSVP unexpectedly created a saved activity.'
    }
    $savedID = (Request 'POST' '/v1/me/saved' @{kind='activity';targetId=$activityID} $studentToken @(201)).data.id
    $afterSaved = (Request 'GET' '/v1/me/saved' $null $studentToken @(200)).data
    if (@($afterSaved | Where-Object { $_.id -eq $savedID -and $_.targetId -eq $activityID }).Count -ne 1) {
        throw 'Saved activity was not persisted.'
    }
    Request 'DELETE' "/v1/me/saved/$savedID" $null $studentToken @(204) | Out-Null
    $afterUnsave = (Request 'GET' '/v1/me/saved' $null $studentToken @(200)).data
    if (@($afterUnsave | Where-Object { $_.targetId -eq $activityID }).Count -ne 0) {
        throw 'Removing saved activity did not persist.'
    }
    $stillGoing = (Request 'GET' "/v1/activities/$activityID/participations/me" $null $studentToken @(200)).data
    if ($stillGoing.status -ne 'going') { throw 'Save or unsave changed the RSVP.' }
    Write-Output '[PASS] Save, unsave and RSVP independence'

    Write-Output "E2E_ACTIVITY_ID=$activityID"
    Write-Output "E2E_TITLE=$title"
} finally {
    if ($activityID -and -not $KeepTestActivity) {
        try {
            Request 'POST' "/v1/me/organizations/$organizationID/activities/$activityID/cancel" $null $adminToken @(200) | Out-Null
            Write-Output '[CLEANUP] Synthetic activity cancelled.'
        } catch {
            Write-Output "[CLEANUP FAILED] Synthetic activity $activityID needs local review."
        }
    }
}
