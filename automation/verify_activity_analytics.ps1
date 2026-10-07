param([string]$ApiBase = 'http://127.0.0.1:3694')

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1','localhost')) {
    throw 'Synthetic analytics verification requires a loopback API.'
}
$ApiBase = $ApiBase.TrimEnd('/')
function Request([string]$method,[string]$path,[object]$body,[string]$token,[int[]]$expected,[string]$source='') {
    $p = @{ Uri="$ApiBase$path"; Method=$method; SkipHttpErrorCheck=$true }
    $headers = @{}
    if ($token) { $headers.Authorization = "Bearer $token" }
    if ($source) { $headers['X-Birdtie-Entry-Source'] = $source }
    if ($headers.Count) { $p.Headers = $headers }
    if ($null -ne $body) {
        $p.ContentType='application/json'
        $p.Body=ConvertTo-Json -InputObject $body -Depth 8 -Compress
    }
    $r=Invoke-WebRequest @p
    if ($r.StatusCode -notin $expected) {
        $code=''
        try { $code=($r.Content | ConvertFrom-Json).error.code } catch {}
        throw "$method $path returned HTTP $($r.StatusCode) $code"
    }
    if ($r.Content) { return $r.Content | ConvertFrom-Json }
    return $null
}
function DevToken([string]$phone) {
    Request 'POST' '/v1/auth/dev-phone/code' @{phone=$phone} '' @(200) | Out-Null
    return (Request 'POST' '/v1/auth/dev-phone/verify' @{phone=$phone;code='123456'} '' @(200)).data.accessToken
}
if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Local development phone auth is disabled.'
}
$admin=DevToken '13800138045'
$student=DevToken '13800138043'
$org=@((Request 'GET' '/v1/me/organizations' $null $admin @(200)).data |
    Where-Object name -eq 'Birdtie 闭环验收组织（本地测试）' | Select-Object -First 1)[0]
if (-not $org) { throw 'Synthetic pilot organization is absent.' }
$title='活动统计验收（本地测试）'+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
$start=[DateTime]::UtcNow.AddDays(3)
$input=@{
    cityId='aberdeen-gb';placeId='b1700000-0000-4000-8000-000000000004'
    title=$title;summary='仅供本地统计验收。';description='测试展示、详情、报名、收藏。'
    startsAt=$start.ToString('yyyy-MM-ddTHH:mm:ssZ');endsAt=$start.AddHours(2).ToString('yyyy-MM-ddTHH:mm:ssZ')
    timeZone='Europe/London';categoryCode='badminton';capacity=12;priceMinor=0
    currency='';eligibility='';languageCode='zh-CN';visibility='public'
}
$activityID=$null
try {
    $activityID=(Request 'POST' "/v1/me/organizations/$($org.id)/activities" $input $admin @(201)).data.id
    Request 'POST' "/v1/me/organizations/$($org.id)/activities/$activityID/publish" $null $admin @(200) | Out-Null
    Request 'POST' "/v1/activities/$activityID/analytics/events" @{eventType='rsvp';entrySource='now'} '' @(400) | Out-Null
    Request 'POST' "/v1/activities/$activityID/analytics/events" @{eventType='impression';entrySource='raw-gps'} '' @(400) | Out-Null
    Request 'POST' "/v1/activities/$activityID/analytics/events" @{eventType='impression';entrySource='now'} '' @(204) | Out-Null
    Request 'POST' "/v1/activities/$activityID/analytics/events" @{eventType='detail';entrySource='agent'} '' @(204) | Out-Null
    Request 'GET' "/v1/me/organizations/$($org.id)/analytics" $null $student @(403) | Out-Null
    $joined=(Request 'POST' "/v1/activities/$activityID/participations" $null $student @(201) 'agent').data
    Request 'POST' "/v1/activities/$activityID/participations" $null $student @(200) 'agent' | Out-Null
    $saved=(Request 'POST' '/v1/me/saved' @{kind='activity';targetId=$activityID} $student @(201) 'now').data
    Request 'POST' '/v1/me/saved' @{kind='activity';targetId=$activityID} $student @(201) 'now' | Out-Null
    $row=@((Request 'GET' "/v1/me/organizations/$($org.id)/analytics" $null $admin @(200)).data |
        Where-Object activityId -eq $activityID | Select-Object -First 1)[0]
    if (-not $row -or $row.impressions -ne 1 -or $row.detailOpens -ne 1 -or
        $row.rsvps -ne 1 -or $row.saves -ne 1) { throw 'Pilot funnel totals are incorrect.' }
    $bySource=@{}
    foreach($source in $row.sources) { $bySource[$source.source]=$source }
    if ($bySource.now.impressions -ne 1 -or $bySource.now.saves -ne 1 -or
        $bySource.agent.detailOpens -ne 1 -or $bySource.agent.rsvps -ne 1) {
        throw 'Entry source attribution is incorrect.'
    }
    Write-Output '[PASS] Public view events validated; conversions only recorded after successful writes'
    Write-Output '[PASS] Repeated RSVP/save do not duplicate conversions; non-admin metrics denied'
    Write-Output '[PASS] Pilot report totals 1 impression, 1 detail, 1 RSVP, 1 save with Now/Agent sources'
} finally {
    if ($activityID) {
        if ($activityID -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe activity ID.' }
        $sql="BEGIN; DELETE FROM admin_audit_events WHERE resource_type='activity' AND resource_id='$activityID' AND organization_id='$($org.id)'; DELETE FROM inbox_items WHERE target_activity_id='$activityID'; DELETE FROM activities WHERE id='$activityID' AND title LIKE '活动统计验收（本地测试）%'; COMMIT;"
        Push-Location (Join-Path $PSScriptRoot '..\apps\api')
        try {
            & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -c $sql | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Synthetic analytics activity cleanup failed.' }
            Write-Output '[CLEANUP] Synthetic activity, events, RSVP and save removed.'
        } finally { Pop-Location }
    }
}
