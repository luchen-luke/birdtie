$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_chat_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
function Sql([string]$db, [string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $db @arguments
    if ($LASTEXITCODE -ne 0) { throw "psql failed in $db ($LASTEXITCODE)" }
}
Push-Location $api
try {
    Sql 'postgres' @('-c', "CREATE DATABASE $database") | Out-Null
    $migrations = @(Get-ChildItem migrations -Filter '*.sql' |
        Where-Object { $_.Name -notlike '*.down.sql' } | Sort-Object Name)
    foreach ($migration in $migrations | Where-Object { [int]$_.Name.Substring(0, 3) -le 29 }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    Sql $database @('-f', '/dev-seeds/001_badminton.sql') | Out-Null
    Sql $database @('-f', '/dev-seeds/002_functional_mvp.sql') | Out-Null
    foreach ($migration in $migrations | Where-Object {
        $number = [int]$_.Name.Substring(0, 3); $number -ge 30 -and $number -le 32
    }) { Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null }
    Sql $database @('-f', '/migrations/033_context_graph.sql') | Out-Null
    Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null
    Sql $database @('-f', '/migrations/034_person_ties.sql') | Out-Null
    $beforeActivities = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $beforeMessages = (Sql $database @('-Atc', 'SELECT count(*) FROM conversation_messages')).Trim()
    $oldRequest = (Sql $database @('-qAtc', "INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at)
        SELECT p.id,q.id,'aberdeen-gb','Synthetic 035 legacy chat','conversation','accepted',now()+interval '1 day'
        FROM (SELECT id FROM accounts WHERE account_type='person' ORDER BY id LIMIT 1) p
        CROSS JOIN (SELECT id FROM accounts WHERE account_type='person' ORDER BY id OFFSET 1 LIMIT 1) q
        RETURNING id")).Trim()
    $oldConversation = (Sql $database @('-qAtc', "INSERT INTO conversations(request_id,member_a_account_id,member_b_account_id)
        SELECT id,sender_account_id,recipient_account_id FROM connection_requests WHERE id='$oldRequest'
        RETURNING id")).Trim()
    $oldMessage = (Sql $database @('-qAtc', "INSERT INTO conversation_messages(conversation_id,sender_account_id,body)
        SELECT id,member_a_account_id,'Synthetic pre-035 message' FROM conversations WHERE id='$oldConversation'
        RETURNING id")).Trim()
    Sql $database @('-f', '/migrations/035_conversation_read_state.sql') | Out-Null
    $members = (Sql $database @('-Atc', "SELECT count(*) FROM conversation_member_states WHERE conversation_id='$oldConversation'")).Trim()
    $legacyUnread = (Sql $database @('-Atc', "SELECT count(*) FROM conversation_messages m
        JOIN conversation_member_states s ON s.conversation_id=m.conversation_id
        WHERE s.conversation_id='$oldConversation' AND m.created_at>s.baseline_at")).Trim()
    $afterActivities = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $afterMessages = (Sql $database @('-Atc', 'SELECT count(*) FROM conversation_messages')).Trim()
    if ($members -ne '2' -or $legacyUnread -ne '0' -or $afterActivities -ne $beforeActivities -or
        [int]$afterMessages -ne ([int]$beforeMessages + 1)) { throw '035 legacy backfill or counts mismatch' }
    Write-Output "[PASS] 035 backfills two member states; historical read status unknown, old Activity $beforeActivities and Message $afterMessages preserved"
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    try {
        & go test -p 1 ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw '035 full Go test failed' }
    } finally { Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue }
    Sql $database @('-c', "UPDATE conversation_member_states SET last_read_message_id='$oldMessage',last_read_at=now()
        WHERE conversation_id='$oldConversation'") | Out-Null
    $guardOutput = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/035_conversation_read_state.down.sql 2>&1 | Out-String)
    if ($LASTEXITCODE -eq 0 -or $guardOutput -notmatch 'cannot remove Chat read state while user cursors exist') {
        throw '035 down did not refuse real read cursors'
    }
    Write-Output '[PASS] 035 down refuses to erase read cursors'
    Sql $database @('-c', "UPDATE conversation_member_states SET last_read_message_id=NULL,last_read_at=NULL
        WHERE conversation_id='$oldConversation'") | Out-Null
    Sql $database @('-f', '/migrations/035_conversation_read_state.down.sql') | Out-Null
    Sql $database @('-f', '/migrations/035_conversation_read_state.sql') | Out-Null
    $members = (Sql $database @('-Atc', "SELECT count(*) FROM conversation_member_states WHERE conversation_id='$oldConversation'")).Trim()
    if ($members -ne '2') { throw '035 down/reapply lost legacy members' }
    Write-Output '[PASS] 035 down and reapply on disposable seeded database'
}
finally {
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
