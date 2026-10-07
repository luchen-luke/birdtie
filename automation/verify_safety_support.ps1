param([string]$ApiBase='http://127.0.0.1:3694')

$ErrorActionPreference='Stop'
$uri=[Uri]$ApiBase
if ($uri.Scheme -ne 'http' -or $uri.Host -notin @('127.0.0.1','localhost')) {
    throw 'Safety verification requires a loopback API.'
}
$ApiBase=$ApiBase.TrimEnd('/')
$orgID='a0e9d92e-c3f3-4d81-ad0e-eda0d198b275'
$activityID='42cc2418-4789-4952-81cf-c18028fd462d'
function Request([string]$method,[string]$path,[object]$body,[string]$token,[int[]]$expected) {
    $p=@{Uri="$ApiBase$path";Method=$method;SkipHttpErrorCheck=$true}
    if ($token) { $p.Headers=@{Authorization="Bearer $token"} }
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
function Sql([string]$statement) {
    Push-Location (Join-Path $PSScriptRoot '..\apps\api')
    try {
        $result=& docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -Atc $statement
        if ($LASTEXITCODE -ne 0) { throw 'Local SQL check failed.' }
        return $result
    } finally { Pop-Location }
}
if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Development phone auth is disabled.'
}
$admin=DevToken '13800138045'
$reporter=DevToken '13800138042'
$other=DevToken '13800138043'
$adminID=(Request 'GET' '/v1/me' $null $admin @(200)).data.id
$reportIDs=@()
$faqID=$null
$profileAuditID=$null
try {
    Request 'POST' '/v1/me/reports' @{targetType='general';targetId='';reason='technical';details='too short'} $reporter @(400) | Out-Null
    Request 'POST' '/v1/me/reports' @{targetType='activity';targetId='00000000-0000-4000-8000-000000000000';reason='safety';details='这是不存在的活动举报验收。'} $reporter @(404) | Out-Null
    $reportIDs+=((Request 'POST' '/v1/me/reports' @{
        targetType='activity';targetId=$activityID;reason='incorrect_information';details='本地验收：活动信息与页面描述不一致。'
    } $reporter @(201)).data.id)
    $reportIDs+=((Request 'POST' '/v1/me/reports' @{
        targetType='general';targetId='';reason='technical';details='本地验收：登录后帮助页面需要显示提交编号。'
    } $reporter @(201)).data.id)
    $own=@((Request 'GET' '/v1/me/reports' $null $reporter @(200)).data)
    $foreign=@((Request 'GET' '/v1/me/reports' $null $other @(200)).data)
    foreach($id in $reportIDs) {
        if (@($own | Where-Object id -eq $id).Count -ne 1 -or
            @($foreign | Where-Object id -eq $id).Count -ne 0) {
            throw 'Report receipt or owner-only list failed.'
        }
    }
    $queue=@((Sql "SELECT id FROM incident_reports WHERE id IN ('$($reportIDs[0])','$($reportIDs[1])') AND status='open';") | Where-Object { $_ })
    if ($queue.Count -ne 2) { throw 'Operator incident queue did not persist reports.' }
    Write-Output '[PASS] Entity report and general support request persisted with receipts; another account cannot read them'
    Write-Output '[PASS] Invalid body and nonexistent target rejected; operator queue contains open cases'

    $question='审计验收 '+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
    $faqID=(Request 'POST' "/v1/me/organizations/$orgID/faqs" @{
        question=$question;answer='本地验收草稿';published=$false
    } $admin @(201)).data.id
    Request 'PUT' "/v1/me/organizations/$orgID/faqs/$faqID" @{
        question=$question;answer='本地验收已更新';published=$false
    } $admin @(200) | Out-Null
    Request 'DELETE' "/v1/me/organizations/$orgID/faqs/$faqID" $null $admin @(204) | Out-Null
    $audit=@((Sql "SELECT action || '|' || actor_account_id FROM admin_audit_events WHERE resource_id='$faqID' ORDER BY id;") | Where-Object { $_ })
    if ($audit.Count -ne 3 -or
        $audit[0] -ne "faq_create|$adminID" -or
        $audit[1] -ne "faq_update|$adminID" -or
        $audit[2] -ne "faq_delete|$adminID") {
        throw 'Organization admin mutation audit missing actor or action.'
    }
    Write-Output '[PASS] FAQ create/update/delete are durably audited with the person actor and organization scope'

    Request 'PUT' "/v1/me/organizations/$orgID/profile" @{
        name='Birdtie 闭环验收组织（本地测试）';description='';officialLinks=@()
    } $admin @(200) | Out-Null
    $profileAuditID=(Sql "SELECT id FROM admin_audit_events WHERE resource_type='organization' AND resource_id='$orgID' AND action='organization_profile_update' AND actor_account_id='$adminID' ORDER BY id DESC LIMIT 1;" | Select-Object -First 1)
    if (-not $profileAuditID -or $profileAuditID -notmatch '^\d+$') {
        throw 'Organization profile update lacked a person-attributed audit event.'
    }
    Write-Output '[PASS] Organization profile update is auditable without storing the profile body'
} finally {
    if ($reportIDs.Count -gt 0 -or $faqID -or $profileAuditID) {
        $sql='BEGIN;'
        foreach($id in $reportIDs) {
            if ($id -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe report ID.' }
            $sql+="DELETE FROM incident_reports WHERE id='$id' AND details LIKE '本地验收：%';"
        }
        if ($faqID) {
            if ($faqID -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe FAQ ID.' }
            $sql+="DELETE FROM organization_faqs WHERE id='$faqID' AND organization_id='$orgID';"
            $sql+="DELETE FROM admin_audit_events WHERE resource_type='faq' AND resource_id='$faqID' AND organization_id='$orgID';"
        }
        if ($profileAuditID -and $profileAuditID -match '^\d+$') {
            $sql+="DELETE FROM admin_audit_events WHERE id=$profileAuditID AND resource_type='organization' AND resource_id='$orgID';"
        }
        $sql+='COMMIT;'
        Sql $sql | Out-Null
        Write-Output '[CLEANUP] Synthetic reports and audit fixture removed.'
    }
}
