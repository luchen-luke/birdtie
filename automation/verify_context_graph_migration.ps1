$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_ctx_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)

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
    Sql $database @('-c', "INSERT INTO agent_tasks
        (owner_account_id,principal_type,acting_user_account_id,city_id,city_context_id,query,status)
        SELECT a.id,'person',a.id,'aberdeen-gb','aberdeen-gb','Legacy Context backfill fixture','ACTIVE'
        FROM accounts a WHERE a.account_type='person' AND a.status='active'
        ORDER BY a.id LIMIT 1") | Out-Null
    $before = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $oldTasks = (Sql $database @('-Atc', 'SELECT count(*) FROM agent_tasks')).Trim()
    if ([int]$oldTasks -lt 1) { throw '033 backfill fixture Agent Task was not created' }
    Sql $database @('-f', '/migrations/033_context_graph.sql') | Out-Null
    $after = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $newTasks = (Sql $database @('-Atc', 'SELECT count(*) FROM agent_tasks')).Trim()
    if ($before -ne $after -or $oldTasks -ne $newTasks) {
        throw "033 changed old Activity/Agent Task count: $before/$after, $oldTasks/$newTasks"
    }
    $mapped = (Sql $database @('-Atc', "SELECT count(*) FROM agent_tasks WHERE context_type='CITY' AND context_id IS NOT NULL")).Trim()
    if ($mapped -ne $newTasks) { throw "033 did not map legacy tasks: $mapped/$newTasks" }
    Write-Output "[PASS] 033 backfill: Activities $before -> $after; Agent Tasks $oldTasks -> $newTasks, mapped $mapped"
    Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null
    Sql $database @('-c', "WITH person AS (
        INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id)
        INSERT INTO agents(agent_type,principal_account_id)
        SELECT 'personal',id FROM person") | Out-Null
    $sql = Get-Content 'test-sql/033_context_graph.sql' -Raw -Encoding utf8
    $sql | docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database | Out-Null
    if ($LASTEXITCODE -ne 0) { throw '033 SQL invariants failed' }
    Write-Output '[PASS] Context shape, private Person link, old city writer, cross-city and online Agent Tasks'
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    try {
        & go test ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw '033 full Go test failed' }
    } finally { Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue }
    Sql $database @('-c', "INSERT INTO contexts(context_type,online_key)
        VALUES('ONLINE','synthetic-down-guard')") | Out-Null
    $guardOutput = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/033_context_graph.down.sql 2>&1 | Out-String)
    if ($LASTEXITCODE -eq 0 -or $guardOutput -notmatch 'cannot remove Context Graph while new context data exists') {
        throw '033 down did not explicitly refuse new Context data'
    }
    Sql $database @('-c', "DELETE FROM contexts WHERE context_type='ONLINE'
        AND online_key='synthetic-down-guard'") | Out-Null
    Write-Output '[PASS] 033 down refuses to discard new non-city Context data'
    Sql $database @('-f', '/migrations/033_context_graph.down.sql') | Out-Null
    Sql $database @('-f', '/migrations/033_context_graph.sql') | Out-Null
    $reapplied = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    if ($reapplied -ne $before) { throw "033 down/reapply changed Activity count: $before/$reapplied" }
    Write-Output '[PASS] 033 down and reapply on disposable seeded database'
}
finally {
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
