param(
    [string]$ApiBase = 'http://127.0.0.1:3694',
    [string]$AdminPhone = '13800138045',
    [string]$StudentPhone = '13800138042',
    [string]$OfflineGatePath = '',
    [switch]$KeepTestData
)

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1', 'localhost')) {
    throw 'Synthetic notifications can only be checked against a loopback API.'
}
$ApiBase = $ApiBase.TrimEnd('/')

function Request([string]$method, [string]$path, [object]$body, [string]$token, [int[]]$expected) {
    $parameters = @{ Uri = "$ApiBase$path"; Method = $method; SkipHttpErrorCheck = $true }
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
    return (Request 'POST' '/v1/auth/dev-phone/verify' @{phone=$phone;code='123456'} '' @(200)).data.accessToken
}

function ItemsFor([string]$token, [string]$id) {
    return @((Request 'GET' '/v1/me/inbox' $null $token @(200)).data |
        Where-Object { $_.targetActivityId -eq $id })
}

function RunReminders {
    $previous = [Environment]::GetEnvironmentVariable('BIRDTIE_DATABASE_URL', 'Process')
    $apiDir = Join-Path $PSScriptRoot '..\apps\api'
    try {
        $env:BIRDTIE_DATABASE_URL = 'postgres://birdtie:birdtie_local_only@127.0.0.1:55432/birdtie?sslmode=disable'
        Push-Location $apiDir
        & go run ./cmd/activity-reminders
        if ($LASTEXITCODE -ne 0) { throw 'Reminder worker failed.' }
    } finally {
        Pop-Location
        [Environment]::SetEnvironmentVariable('BIRDTIE_DATABASE_URL', $previous, 'Process')
    }
}

if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Local development phone authentication is disabled.'
}
Request 'GET' '/readyz' $null '' @(200) | Out-Null
$adminToken = DevToken $AdminPhone
$studentToken = DevToken $StudentPhone
$organization = @((Request 'GET' '/v1/me/organizations' $null $adminToken @(200)).data |
    Where-Object { $_.name -eq 'Birdtie 闭环验收组织（本地测试）' }) | Select-Object -First 1
if (-not $organization) { throw 'Run the vertical slice check first to create the synthetic admin organization.' }
$organizationID = $organization.id
$title = '活动通知验收（本地测试）' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
$start = [DateTime]::UtcNow.AddMinutes(80)
$end = $start.AddHours(1)
$input = @{
    cityId='aberdeen-gb'; placeId='b1700000-0000-4000-8000-000000000004'
    title=$title; summary='仅供本地活动通知验收。'; description='测试提醒、变更与取消。'
    startsAt=$start.ToString('yyyy-MM-ddTHH:mm:ssZ'); endsAt=$end.ToString('yyyy-MM-ddTHH:mm:ssZ')
    timeZone='Europe/London'; categoryCode='badminton'; capacity=12; priceMinor=0
    currency=''; eligibility=''; languageCode='zh-CN'; visibility='public'
}
$activityID = $null
try {
    $activityID = (Request 'POST' "/v1/me/organizations/$organizationID/activities" $input $adminToken @(201)).data.id
    Request 'POST' "/v1/me/organizations/$organizationID/activities/$activityID/publish" $null $adminToken @(200) | Out-Null
    Request 'POST' "/v1/activities/$activityID/participations" $null $studentToken @(201) | Out-Null
    Write-Output "[PASS] Published, joined synthetic activity: $activityID"

    if ($OfflineGatePath) {
        Write-Output "[WAIT] Stop the API, run the independent worker, restart the API, then create $OfflineGatePath"
        $deadline = [DateTime]::UtcNow.AddMinutes(4)
        while (-not (Test-Path -LiteralPath $OfflineGatePath)) {
            if ([DateTime]::UtcNow -ge $deadline) { throw 'Offline worker gate timed out.' }
            Start-Sleep -Seconds 1
        }
        $offlineItems = ItemsFor $studentToken $activityID
        if (@($offlineItems | Where-Object resourceType -eq 'activity_reminder').Count -ne 1) {
            throw 'Offline worker reminder did not persist through API restart.'
        }
        Write-Output '[PASS] Independent worker delivered during API downtime; Inbox survived restart'
    }

    RunReminders
    RunReminders
    $items = ItemsFor $studentToken $activityID
    if (@($items | Where-Object resourceType -eq 'activity_reminder').Count -ne 1) {
        throw 'Starts-soon reminder did not arrive exactly once.'
    }
    Write-Output '[PASS] Starts-soon reminder is persisted and idempotent'

    $input.startsAt = $start.AddMinutes(10).ToString('yyyy-MM-ddTHH:mm:ssZ')
    $input.endsAt = $end.AddMinutes(10).ToString('yyyy-MM-ddTHH:mm:ssZ')
    Request 'PUT' "/v1/me/organizations/$organizationID/activities/$activityID" $input $adminToken @(200) | Out-Null
    $items = ItemsFor $studentToken $activityID
    if (@($items | Where-Object title -eq '活动时间已调整').Count -ne 1 -or
        @($items | Where-Object resourceType -eq 'activity_reminder').Count -ne 0) {
        throw 'Time change notification or stale reminder invalidation failed.'
    }
    RunReminders
    $items = ItemsFor $studentToken $activityID
    if (@($items | Where-Object resourceType -eq 'activity_reminder').Count -ne 1) {
        throw 'Reminder was not recreated for the new start time.'
    }
    Write-Output '[PASS] Time change notified and reminder rescheduled'

    $input.placeId = 'b1700000-0000-4000-8000-000000000007'
    Request 'PUT' "/v1/me/organizations/$organizationID/activities/$activityID" $input $adminToken @(200) | Out-Null
    $items = ItemsFor $studentToken $activityID
    if (@($items | Where-Object title -eq '活动地点已调整').Count -ne 1) {
        throw 'Venue change notification missing.'
    }
    Write-Output '[PASS] Venue change notified'

    $notification = $items | Where-Object title -eq '活动地点已调整' | Select-Object -First 1
    $read = (Request 'POST' "/v1/me/inbox/$($notification.id)/read" $null $studentToken @(200)).data
    if (-not $read.readAt -or $read.targetActivityId -ne $activityID) {
        throw 'Inbox read state or activity link did not persist.'
    }
    $otherInbox = ItemsFor $adminToken $activityID
    if (@($otherInbox).Count -ne 0) { throw 'Non-participant received activity notification.' }
    Write-Output '[PASS] Inbox read state, activity link and participant targeting'

    Request 'POST' "/v1/me/organizations/$organizationID/activities/$activityID/cancel" $null $adminToken @(200) | Out-Null
    RunReminders
    $items = ItemsFor $studentToken $activityID
    $detail = (Request 'GET' "/v1/activities/$activityID" $null $studentToken @(200)).data
    if (@($items | Where-Object resourceType -eq 'activity_cancelled').Count -ne 1 -or
        @($items | Where-Object resourceType -eq 'activity_reminder').Count -ne 0 -or
        $detail.status -ne 'cancelled') {
        throw 'Cancellation notification, stale reminder removal, or linked detail failed.'
    }
    Push-Location (Join-Path $PSScriptRoot '..\apps\api')
    try {
        $audit = @(& docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -Atc "SELECT action FROM admin_audit_events WHERE resource_type='activity' AND resource_id='$activityID' ORDER BY id;")
        if ($LASTEXITCODE -ne 0) { throw 'Activity audit query failed.' }
    } finally { Pop-Location }
    if (@($audit | Where-Object { $_ -eq 'activity_create' }).Count -ne 1 -or
        @($audit | Where-Object { $_ -eq 'activity_publish' }).Count -ne 1 -or
        @($audit | Where-Object { $_ -eq 'activity_update' }).Count -ne 2 -or
        @($audit | Where-Object { $_ -eq 'activity_cancel' }).Count -ne 1) {
        throw 'Activity create, publish, edit or cancel audit missing.'
    }
    Write-Output '[PASS] Cancellation notified; linked detail shows cancelled; no stale reminder'
    Write-Output '[PASS] Activity create/publish/edit/cancel mutations audited'
    Write-Output "NOTIFICATION_ACTIVITY_ID=$activityID"
} finally {
    if ($activityID -and -not $KeepTestData) {
        if ($activityID -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe synthetic activity ID.' }
        $sql = "BEGIN; DELETE FROM admin_audit_events WHERE resource_type='activity' AND resource_id='$activityID' AND organization_id='$organizationID'; DELETE FROM inbox_items WHERE target_activity_id='$activityID'; DELETE FROM activities WHERE id='$activityID' AND title LIKE '活动通知验收（本地测试）%'; COMMIT;"
        Push-Location (Join-Path $PSScriptRoot '..\apps\api')
        try {
            & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -c $sql | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Synthetic activity cleanup failed.' }
            Write-Output '[CLEANUP] Synthetic activity and its notifications removed.'
        } finally { Pop-Location }
    }
}
