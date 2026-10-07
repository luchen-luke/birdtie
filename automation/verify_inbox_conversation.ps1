param(
    [string]$ApiBase = 'http://127.0.0.1:3694',
    [switch]$KeepTestData
)

$ErrorActionPreference = 'Stop'
$baseUri = [Uri]$ApiBase
if ($baseUri.Scheme -ne 'http' -or $baseUri.Host -notin @('127.0.0.1', 'localhost')) {
    throw 'Synthetic conversation verification requires a loopback API.'
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
    $response = Invoke-WebRequest @p
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

function LocalSql([string]$sql) {
    Push-Location $apiDir
    try {
        $result = & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -Atc $sql
        if ($LASTEXITCODE -ne 0) { throw 'Local test SQL failed.' }
        return $result
    } finally { Pop-Location }
}

if (-not (Request 'GET' '/v1/auth/dev-phone/status' $null '' @(200)).data.enabled) {
    throw 'Local development phone authentication is disabled.'
}
$recipientToken = DevToken '13800138042'
$senderToken = DevToken '13800138046'
$recipientID = (Request 'GET' '/v1/me' $null $recipientToken @(200)).data.id
$senderID = (Request 'GET' '/v1/me' $null $senderToken @(200)).data.id
foreach ($id in @($recipientID,$senderID)) {
    if ($id -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe account ID.' }
}
$requestID = $null
$conversationID = $null
try {
    $requestID = (LocalSql "INSERT INTO connection_requests (sender_account_id,recipient_account_id,city_id,note,state,expires_at,decided_at) VALUES ('$senderID','$recipientID','aberdeen-gb','本地收件箱验收','accepted',now()+interval '1 day',now()) RETURNING id;") | Select-Object -First 1
    if ($requestID -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe test request ID.' }
    $conversationID = (LocalSql "INSERT INTO conversations (request_id,member_a_account_id,member_b_account_id) VALUES ('$requestID','$senderID','$recipientID') RETURNING id;") | Select-Object -First 1
    if ($conversationID -notmatch '^[0-9a-f-]{36}$') { throw 'Unsafe test conversation ID.' }

    $message = (Request 'POST' "/v1/me/conversations/$conversationID/messages" @{
        body='本地收件箱跳转验收消息'
    } $senderToken @(201)).data
    $items = @((Request 'GET' '/v1/me/inbox' $null $recipientToken @(200)).data |
        Where-Object { $_.resourceId -eq $message.id })
    if ($items.Count -ne 1 -or $items[0].targetConversationId -ne $conversationID -or
        $items[0].title -ne '收到新消息' -or $items[0].readAt) {
        throw 'Conversation target, Chinese title or unread state is invalid.'
    }
    Write-Output '[PASS] New human message has an unread Chinese Inbox item linked to its conversation'

    $visible = @((Request 'GET' '/v1/me/conversations' $null $recipientToken @(200)).data |
        Where-Object { $_.id -eq $conversationID })
    $messages = @((Request 'GET' "/v1/me/conversations/$conversationID/messages" $null $recipientToken @(200)).data |
        Where-Object { $_.id -eq $message.id })
    if ($visible.Count -ne 1 -or $messages.Count -ne 1) {
        throw 'Inbox target cannot be opened as a real conversation.'
    }
    $read = (Request 'POST' "/v1/me/inbox/$($items[0].id)/read" $null $recipientToken @(200)).data
    if (-not $read.readAt -or $read.targetConversationId -ne $conversationID) {
        throw 'Inbox read state or target was not persisted.'
    }
    Write-Output '[PASS] Conversation is accessible to its member and Inbox read state persists'

    $otherItems = @((Request 'GET' '/v1/me/inbox' $null $senderToken @(200)).data |
        Where-Object { $_.resourceId -eq $message.id })
    if ($otherItems.Count -ne 0) { throw 'Sender received their own message notification.' }
    Write-Output '[PASS] Message notification only reaches the recipient'
    Write-Output "INBOX_CONVERSATION_ID=$conversationID"
} finally {
    if ($requestID -and $requestID -match '^[0-9a-f-]{36}$' -and
        $conversationID -and $conversationID -match '^[0-9a-f-]{36}$' -and
        -not $KeepTestData) {
        LocalSql "BEGIN; DELETE FROM inbox_items WHERE target_conversation_id='$conversationID'; DELETE FROM conversation_messages WHERE conversation_id='$conversationID'; DELETE FROM conversations WHERE id='$conversationID'; DELETE FROM connection_requests WHERE id='$requestID' AND note='本地收件箱验收'; COMMIT;" | Out-Null
        Write-Output '[CLEANUP] Synthetic conversation and notification removed.'
    } elseif ($requestID -and -not $conversationID) {
        LocalSql "DELETE FROM connection_requests WHERE id='$requestID' AND note='本地收件箱验收';" | Out-Null
    }
}
