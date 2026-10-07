param(
    [string]$ApiBase = 'http://127.0.0.1:3694',
    [string]$OrganizationID = 'a0e9d92e-c3f3-4d81-ad0e-eda0d198b275',
    [switch]$KeepTestData
)

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1', 'localhost') -or
    $OrganizationID -notmatch '^[0-9a-f-]{36}$') {
    throw 'Synthetic FAQ verification requires a loopback API and UUID.'
}
$ApiBase = $ApiBase.TrimEnd('/')
$apiDir = Join-Path $PSScriptRoot '..\apps\api'
function Request([string]$method, [string]$path, [object]$body, [string]$token, [int[]]$expected) {
    $p = @{ Uri="$ApiBase$path"; Method=$method; SkipHttpErrorCheck=$true }
    if ($null -ne $body) {
        $p.ContentType = 'application/json'
        $p.Body = ConvertTo-Json -InputObject $body -Depth 8 -Compress
    }
    if ($token) { $p.Headers = @{ Authorization="Bearer $token" } }
    $r = Invoke-WebRequest @p
    if ($r.StatusCode -notin $expected) {
        $code = ''
        try { $code = ($r.Content | ConvertFrom-Json).error.code } catch {}
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
    Push-Location $apiDir
    try {
        $out = & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -Atc $statement
        if ($LASTEXITCODE -ne 0) { throw 'Local test SQL failed.' }
        return $out
    } finally { Pop-Location }
}
function Ask([string]$query) {
    return (Request 'POST' "/v1/organizations/$OrganizationID/agent/ask" @{query=$query} '' @(200)).data
}

if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Local development phone authentication is disabled.'
}
$baseline = (Sql "SELECT name || '|' || verification_status || '|' || visibility || '|' || description || '|' || official_links::text FROM organizations WHERE id='$OrganizationID';" | Select-Object -First 1)
if ($baseline -ne 'Birdtie 闭环验收组织（本地测试）|unverified|public||[]') {
    throw 'Expected untouched synthetic organization; refusing to change other data.'
}
$adminToken = DevToken '13800138045'
$studentToken = DevToken '13800138042'
$faqID = $null
$deleteID = $null
try {
    $question = '如何报名？'
    $originalAnswer = '请在活动详情点击“报名参加”。'
    $faqID = (Request 'POST' "/v1/me/organizations/$OrganizationID/faqs" @{
        question=$question;answer=$originalAnswer;published=$false
    } $adminToken @(201)).data.id
    if ($faqID -notmatch '^[0-9a-f-]{36}$') { throw 'Invalid FAQ ID.' }
    $listed = @((Request 'GET' "/v1/me/organizations/$OrganizationID/faqs" $null $adminToken @(200)).data |
        Where-Object { $_.id -eq $faqID })
    if ($listed.Count -ne 1 -or $listed[0].published) { throw 'Admin draft FAQ did not persist.' }
    Request 'POST' "/v1/me/organizations/$OrganizationID/faqs" @{
        question='越权测试？';answer='不应保存';published=$true
    } $studentToken @(403) | Out-Null
    if ((Ask $question).status -ne 'unknown') { throw 'Unverified Organization Agent answered a draft.' }
    Write-Output '[PASS] Admin draft, ownership guard and unverified unknown'

    Request 'PUT' "/v1/me/organizations/$OrganizationID/faqs/$faqID" @{
        question=$question;answer=$originalAnswer;published=$true
    } $adminToken @(200) | Out-Null
    if ((Ask $question).status -ne 'unknown') { throw 'Unverified Organization Agent answered a published FAQ.' }
    Sql "UPDATE organizations SET verification_status='verified', description='本地测试组织简介', official_links=jsonb_build_array('https://example.org/birdtie-test') WHERE id='$OrganizationID';" | Out-Null

    $faqAnswer = Ask '如何报名'
    if ($faqAnswer.status -ne 'known' -or $faqAnswer.answer -ne $originalAnswer -or
        @($faqAnswer.sources).Count -ne 1 -or $faqAnswer.sources[0].type -ne 'faq' -or
        $faqAnswer.sources[0].id -ne $faqID) {
        throw 'Published FAQ answer or source is incorrect.'
    }
    $activityAnswer = Ask '近期有什么活动？'
    if ($activityAnswer.status -ne 'known' -or @($activityAnswer.sources).Count -ne 1 -or
        $activityAnswer.sources[0].type -ne 'activity') { throw 'Public Activity answer missing source.' }
    $profileAnswer = Ask '组织介绍'
    if ($profileAnswer.answer -ne '本地测试组织简介' -or $profileAnswer.sources[0].type -ne 'profile') {
        throw 'Profile answer missing source.'
    }
    $linkAnswer = Ask '官网'
    if ($linkAnswer.sources[0].url -ne 'https://example.org/birdtie-test') {
        throw 'Organization-provided link missing source URL.'
    }
    Write-Output '[PASS] Verified FAQ, Activity, profile and link answers cite stored public sources'

    foreach ($unknown in @('退款政策？','周末羽毛球退款政策','附近有没有免费活动？')) {
        $answer = Ask $unknown
        if ($answer.status -ne 'unknown' -or @($answer.sources).Count -ne 0) {
            throw "Unsupported question $unknown was guessed."
        }
    }
    Write-Output '[PASS] Unsupported policy and filtered-event questions explicitly remain unknown'

    $newAnswer = '请先阅读活动详情，再点击“报名参加”。'
    Request 'PUT' "/v1/me/organizations/$OrganizationID/faqs/$faqID" @{
        question=$question;answer=$newAnswer;published=$true
    } $adminToken @(200) | Out-Null
    if ((Ask '如何报名').answer -ne $newAnswer) { throw 'Admin FAQ edit was not reflected.' }
    $deleteID = (Request 'POST' "/v1/me/organizations/$OrganizationID/faqs" @{
        question='本地删除测试？';answer='应被删除';published=$true
    } $adminToken @(201)).data.id
    Request 'DELETE' "/v1/me/organizations/$OrganizationID/faqs/$deleteID" $null $adminToken @(204) | Out-Null
    if ((Ask '本地删除测试').status -ne 'unknown') { throw 'Deleted FAQ still answers.' }
    Write-Output '[PASS] Admin FAQ edit and delete are effective'
    Write-Output "FAQ_ID=$faqID"
} finally {
    if ($deleteID -and $deleteID -match '^[0-9a-f-]{36}$') {
        Sql "DELETE FROM organization_faqs WHERE id='$deleteID' AND organization_id='$OrganizationID';" | Out-Null
        Sql "DELETE FROM admin_audit_events WHERE resource_type='faq' AND resource_id='$deleteID' AND organization_id='$OrganizationID';" | Out-Null
    }
    if (-not $KeepTestData) {
        if ($faqID -and $faqID -match '^[0-9a-f-]{36}$') {
            Sql "DELETE FROM organization_faqs WHERE id='$faqID' AND organization_id='$OrganizationID';" | Out-Null
            Sql "DELETE FROM admin_audit_events WHERE resource_type='faq' AND resource_id='$faqID' AND organization_id='$OrganizationID';" | Out-Null
        }
        Sql "UPDATE organizations SET verification_status='unverified', description='', official_links='[]'::jsonb WHERE id='$OrganizationID' AND name='Birdtie 闭环验收组织（本地测试）';" | Out-Null
        Write-Output '[CLEANUP] Synthetic FAQ and Organization verification fixture restored.'
    }
}
