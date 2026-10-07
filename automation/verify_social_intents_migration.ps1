param([ValidateSet(36,37,38)][int]$Through = 36)
$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_social_intent_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
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
        $number = [int]$_.Name.Substring(0, 3); $number -ge 30 -and $number -le $Through
    }) {
        if ($migration.Name -eq '036_social_intents.sql') {
            $legacy = (Sql $database @('-qAtc', "INSERT INTO intents
                (owner_account_id,city_id,topic,details,available_from,available_until,
                 time_zone,coarse_area_label,audience,state,expires_at)
                SELECT id,'aberdeen-gb','Synthetic legacy intent','migration guard',
                    now(),now()+interval '1 day','Europe/London','Aberdeen',
                    'private','draft',now()+interval '1 day'
                FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1
                RETURNING id")).Trim()
            if (!$legacy) { throw 'no person account for legacy Intent guard' }
        }
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
        if ($migration.Name -eq '033_context_graph.sql') {
            Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null
        }
    }
    $oldActivities = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $oldIntents = (Sql $database @('-Atc', 'SELECT count(*) FROM intents')).Trim()
    $oldIDs = (Sql $database @('-Atc', "SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents")).Trim()
    $tables = (Sql $database @('-Atc', "SELECT to_regclass('public.social_intents') IS NOT NULL")).Trim()
    if ($tables -ne 't') { throw '036 table missing' }
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    try {
        & go test -p 1 ./... -count=1
        if ($LASTEXITCODE -ne 0) { throw '036 full Go test failed' }
    } finally { Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue }
    $afterActivities = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $afterIntents = (Sql $database @('-Atc', 'SELECT count(*) FROM intents')).Trim()
    $afterIDs = (Sql $database @('-Atc', "SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM intents")).Trim()
    if ($oldActivities -ne $afterActivities -or $oldIntents -ne $afterIntents -or $oldIDs -ne $afterIDs) {
        throw 'legacy Activity or Intent changed by 036 tests'
    }
    Write-Output "[PASS] 036 API/DB tests; old Activity $oldActivities and legacy Intent $oldIntents IDs preserved"
    if ($Through -eq 38) {
        $sourceTask = (Sql $database @('-qAtc', "INSERT INTO agent_tasks
            (owner_account_id,acting_user_account_id,principal_type,city_id,city_context_id,query,intent,status)
            SELECT id,id,'person','aberdeen-gb','aberdeen-gb','Synthetic activity search','FIND_ACTIVITY','COMPLETED'
            FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1 RETURNING id")).Trim()
        if (!$sourceTask) { throw 'no source task for 038 guard' }
        $linkedDraft = (Sql $database @('-qAtc', "INSERT INTO social_intents
            (creator_account_id,intent_type,title,audience,modality,expires_at,source_agent_task_id)
            SELECT owner_account_id,'FIND_ACTIVITY','Synthetic linked draft','PRIVATE','ONLINE',
                now()+interval '1 day',id FROM agent_tasks WHERE id='$sourceTask' RETURNING id")).Trim()
        if (!$linkedDraft) { throw 'no linked draft for 038 guard' }
        $originGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/038_social_intent_agent_origin.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $originGuard -notmatch 'cannot remove social intent AgentTask links') {
            throw '038 down did not protect linked drafts'
        }
        Write-Output '[PASS] 038 down refuses to erase AgentTask provenance'
        Sql $database @('-c', "DELETE FROM social_intents WHERE id='$linkedDraft'") | Out-Null
        Sql $database @('-c', "DELETE FROM agent_tasks WHERE id='$sourceTask'") | Out-Null
        Sql $database @('-f', '/migrations/038_social_intent_agent_origin.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/038_social_intent_agent_origin.sql') | Out-Null
        Write-Output '[PASS] 038 down and reapply on disposable seeded database'
    } elseif ($Through -eq 37) {
        $targetDraft = (Sql $database @('-qAtc', "WITH draft AS (
            INSERT INTO social_intents(creator_account_id,intent_type,title,audience,modality,expires_at)
            SELECT id,'OTHER','Synthetic guarded local draft','LOCAL','ONLINE',now()+interval '1 day'
            FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1
            RETURNING id)
            INSERT INTO social_intent_audience_targets(intent_id,city_id)
            SELECT id,'aberdeen-gb' FROM draft RETURNING intent_id")).Trim()
        if (!$targetDraft) { throw 'no local audience target for guard' }
        $targetGuard = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/037_social_intent_audiences.down.sql 2>&1 | Out-String)
        if ($LASTEXITCODE -eq 0 -or $targetGuard -notmatch 'cannot remove social intent audience data') {
            throw '037 down did not protect audience data'
        }
        Write-Output '[PASS] 037 down refuses to erase audience targets'
        Sql $database @('-c', "DELETE FROM social_intents WHERE id='$targetDraft'") | Out-Null
        Sql $database @('-f', '/migrations/037_social_intent_audiences.down.sql') | Out-Null
        Sql $database @('-f', '/migrations/037_social_intent_audiences.sql') | Out-Null
        Write-Output '[PASS] 037 down and reapply on disposable seeded database'
    } else {
    $draft = (Sql $database @('-qAtc', "INSERT INTO social_intents
        (creator_account_id,intent_type,title,audience,modality,expires_at)
        SELECT id,'OTHER','Synthetic guarded draft','PRIVATE','ONLINE',now()+interval '1 day'
        FROM accounts WHERE account_type='person' AND status='active' ORDER BY id LIMIT 1
        RETURNING id")).Trim()
    if (!$draft) { throw 'no person account for guard' }
    $guardOutput = (& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/036_social_intents.down.sql 2>&1 | Out-String)
    if ($LASTEXITCODE -eq 0 -or $guardOutput -notmatch 'cannot remove V4 social intent while user drafts exist') {
        throw '036 down did not protect user drafts'
    }
    Write-Output '[PASS] 036 down refuses to erase user drafts'
    Sql $database @('-c', "DELETE FROM social_intents WHERE id='$draft'") | Out-Null
    Sql $database @('-f', '/migrations/036_social_intents.down.sql') | Out-Null
    Sql $database @('-f', '/migrations/036_social_intents.sql') | Out-Null
    Write-Output '[PASS] 036 down and reapply on disposable seeded database'
    }
}
finally {
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
