$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_tie_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)

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
    }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    Sql $database @('-f', '/migrations/033_context_graph.sql') | Out-Null
    Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null
    $beforeActivities = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $beforeRequests = (Sql $database @('-Atc', 'SELECT count(*) FROM connection_requests')).Trim()
    $beforeMessages = (Sql $database @('-Atc', 'SELECT count(*) FROM conversation_messages')).Trim()
    Sql $database @('-f', '/migrations/034_person_ties.sql') | Out-Null
    foreach ($pair in @(@('activities', $beforeActivities), @('connection_requests', $beforeRequests), @('conversation_messages', $beforeMessages))) {
        $after = (Sql $database @('-Atc', "SELECT count(*) FROM $($pair[0])")).Trim()
        if ($after -ne $pair[1]) { throw "034 changed existing $($pair[0]) rows: $($pair[1]) -> $after" }
    }
    $tieCount = (Sql $database @('-Atc', 'SELECT count(*) FROM person_ties')).Trim()
    if ($tieCount -ne '0') { throw "034 improperly backfilled $tieCount Ties" }
    Write-Output "[PASS] 034 preserves old Activities $beforeActivities, requests $beforeRequests, messages $beforeMessages; zero automatic Ties"
    $sql = Get-Content 'test-sql/034_person_ties.sql' -Raw -Encoding utf8
    $sql | docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database | Out-Null
    if ($LASTEXITCODE -ne 0) { throw '034 SQL invariants failed' }
    Write-Output '[PASS] Tie shape, old conversation isolation, unique bilateral pair and city-free friend request'
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    try {
        & go test -p 1 ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw '034 full Go test failed' }
    } finally { Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue }
    Sql $database @('-c', "WITH people AS (
        INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person'),(gen_random_uuid(),'person') RETURNING id)
        INSERT INTO connection_requests(sender_account_id,recipient_account_id,city_id,note,scope,expires_at)
        SELECT (array_agg(id))[1],(array_agg(id))[2],NULL,'Synthetic down guard','friend',now()+interval '1 day' FROM people") | Out-Null
    $guardOutput = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/034_person_ties.down.sql 2>&1 | Out-String)
    if ($LASTEXITCODE -eq 0 -or $guardOutput -notmatch 'cannot remove Tie Graph while friend data exists') {
        throw '034 down did not refuse friend data'
    }
    $guardAccounts = (Sql $database @('-Atc', "SELECT string_agg(id::text,',') FROM (
        SELECT sender_account_id AS id FROM connection_requests WHERE note='Synthetic down guard'
        UNION SELECT recipient_account_id FROM connection_requests WHERE note='Synthetic down guard') people")).Trim()
    Sql $database @('-c', "DELETE FROM connection_requests WHERE note='Synthetic down guard'") | Out-Null
    Sql $database @('-c', "DELETE FROM accounts WHERE id=ANY(string_to_array('$guardAccounts',',')::uuid[])") | Out-Null
    Write-Output '[PASS] 034 down refuses to discard friend requests'
    Sql $database @('-f', '/migrations/034_person_ties.down.sql') | Out-Null
    Sql $database @('-f', '/migrations/034_person_ties.sql') | Out-Null
    Write-Output '[PASS] 034 down and reapply on disposable seeded database'
}
finally {
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
