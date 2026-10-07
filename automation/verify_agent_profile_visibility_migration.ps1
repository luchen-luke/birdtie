param([switch]$SkipFullGo, [string]$EvidencePrefix='v5-age003-migration-round1')
$ErrorActionPreference='Stop'
if ($EvidencePrefix -notmatch '^v5-age003-migration-[a-z0-9-]+$') { throw 'invalid field visibility evidence prefix' }
$apiDirectory=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\apps\api')).Path
$outputDirectory=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\work')).Path
$database='birdtie_visibility_verify_'+[Guid]::NewGuid().ToString('N').Substring(0,12)
$freshDatabase=$database+'_fresh'
$createdDatabases=@()
$previousDsn=$env:BIRDTIE_DATABASE_URL
$previousDisposable=$env:BIRDTIE_DISPOSABLE_DB
$proof=@{evidenceClass='LOCAL_DISPOSABLE_SYNTHETIC_ONLY';seededDatabase=$database;freshDatabase=$freshDatabase;fullGoRequested=(!$SkipFullGo)}

function Sql([string]$target,[string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "field visibility verifier SQL failed ($LASTEXITCODE)" }
}
function SqlText([string]$target,[string]$query,[string[]]$arguments=@()) {
    $query | & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "field visibility verifier SQL text failed ($LASTEXITCODE)" }
}
function Snapshot([string]$target,[switch]$IncludePolicy) {
    # Entire rows in every existing public source table, not counts or a subset
    # of columns. Only the new overlay is excluded until IncludePolicy is set.
    $tables=@(Sql $target @('-qAtc',"SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE' AND table_name<>'agent_profile_field_visibility' ORDER BY table_name"))
    $parts=@()
    foreach ($table in $tables) {
        if ($table -notmatch '^[a-z][a-z0-9_]+$') { throw 'unexpected public source table identifier' }
        $parts+=@("SELECT '$table'::text AS name,(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]'::jsonb) FROM public.$table x) AS rows")
    }
    if ($parts.Count -eq 0) { throw 'empty source table snapshot' }
    $query='SELECT jsonb_object_agg(name,rows)::text FROM ('+($parts -join ' UNION ALL ')+') snapshots;'
    $snapshot=(@(SqlText $target $query @('-qAt')) -join "`n").Trim()
    if ($IncludePolicy) {
        $policy=(@(Sql $target @('-qAtc',"SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id)::text,'[]') FROM agent_profile_field_visibility x")) -join "`n").Trim()
        $snapshot+='|'+$policy
    }
    return $snapshot
}
function AssertNoPolicies([string]$target) {
    if ((Sql $target @('-qAtc','SELECT count(*) FROM agent_profile_field_visibility')).Trim() -ne '0') { throw 'migration copied or retained a field visibility rule' }
}
function SaveSnapshotDifference([string]$beforeSnapshot,[string]$afterSnapshot,[string]$stage,[string]$capturedPrimaryKeyJson='') {
    # Keep the exact synthetic database evidence in local files, not console
    # output; the concise summary names changed tables without profile values.
    $beforeSnapshot | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-$stage-before.json")
    $afterSnapshot | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-$stage-after.json")
    $beforeRows=ConvertFrom-Json -InputObject $beforeSnapshot -AsHashtable
    $afterRows=ConvertFrom-Json -InputObject $afterSnapshot -AsHashtable
    $keyQuery="SELECT coalesce(jsonb_object_agg(table_name,columns),'{}'::jsonb) FROM (
        SELECT t.relname AS table_name,jsonb_agg(a.attname ORDER BY key.ordinality) AS columns
        FROM pg_index i JOIN pg_class t ON t.oid=i.indrelid JOIN pg_namespace n ON n.oid=t.relnamespace
        CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS key(attnum,ordinality)
        JOIN pg_attribute a ON a.attrelid=t.oid AND a.attnum=key.attnum
        WHERE n.nspname='public' AND i.indisprimary GROUP BY t.relname) keys"
    $primaryKeyJson=$capturedPrimaryKeyJson
    if ([string]::IsNullOrWhiteSpace($primaryKeyJson)) {
        $primaryKeyJson=(@(Sql $database @('-qAtc',$keyQuery)) -join "`n").Trim()
    }
    $primaryKeyJson | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-$stage-primary-keys.json")
    $primaryKeys=ConvertFrom-Json -InputObject $primaryKeyJson -AsHashtable
    function Digest([object]$value) {
        $serialized=ConvertTo-Json -InputObject $value -Depth 100 -Compress
        return [Convert]::ToHexString([System.Security.Cryptography.SHA256]::HashData([System.Text.Encoding]::UTF8.GetBytes($serialized))).ToLowerInvariant()
    }
    function Key([object]$row,[object[]]$columns) {
        $values=@($columns | ForEach-Object { $row[$_] })
        return ConvertTo-Json -InputObject $values -Depth 100 -Compress
    }
    function KeyFields([object]$row,[object[]]$columns) {
        $result=[ordered]@{}
        foreach ($column in $columns) { $result[$column]=$row[$column] }
        return $result
    }
    $differences=@()
    foreach ($table in @($beforeRows.Keys | Sort-Object)) {
        $oldRows=@($beforeRows[$table])
        $newRows=@($afterRows[$table])
        $oldRowsJson=ConvertTo-Json -InputObject $oldRows -Depth 100 -Compress
        $newRowsJson=ConvertTo-Json -InputObject $newRows -Depth 100 -Compress
        if ($oldRowsJson -cne $newRowsJson) {
            $columns=@($primaryKeys[$table])
            $oldByKey=@{}
            $newByKey=@{}
            if ($columns.Count -gt 0) {
                foreach ($row in $oldRows) { $oldByKey[(Key $row $columns)]=$row }
                foreach ($row in $newRows) { $newByKey[(Key $row $columns)]=$row }
            }
            $removed=@()
            $added=@()
            $changed=@()
            foreach ($identity in @($oldByKey.Keys | Sort-Object)) {
                $old=$oldByKey[$identity]
                if (!$newByKey.ContainsKey($identity)) { $removed+=@(KeyFields $old $columns); continue }
                $new=$newByKey[$identity]
                $changes=@()
                foreach ($field in @($old.Keys | Sort-Object)) {
                    $oldValueHash=Digest $old[$field]
                    $newValueHash=Digest $new[$field]
                    if ($oldValueHash -cne $newValueHash) {
                        $changes+=@([ordered]@{column=$field;beforeHash=$oldValueHash;afterHash=$newValueHash})
                    }
                }
                if ($changes.Count -gt 0) { $changed+=@([ordered]@{key=(KeyFields $old $columns);columns=$changes}) }
            }
            foreach ($identity in @($newByKey.Keys | Sort-Object)) {
                if (!$oldByKey.ContainsKey($identity)) { $added+=@(KeyFields $newByKey[$identity] $columns) }
            }
            $differences+=@([ordered]@{
                table=$table;beforeCount=$oldRows.Count;afterCount=$newRows.Count
                beforeChecksum=(Digest $oldRows);afterChecksum=(Digest $newRows)
                primaryKeyColumns=$columns;removed=$removed;added=$added;changed=$changed
            })
        }
    }
    ConvertTo-Json -InputObject $differences -Depth 10 | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-$stage-difference.json")
    $proof.snapshotDifferenceStage=$stage
    $proof.snapshotDifferenceTables=@($differences | ForEach-Object { $_.table })
}

Push-Location -LiteralPath $apiDirectory
try {
    foreach ($target in @($database,$freshDatabase)) {
        Sql 'postgres' @('-c',"CREATE DATABASE $target") | Out-Null
        $createdDatabases+=@($target)
    }
    $migrations=@(Get-ChildItem -LiteralPath migrations -Filter '*.sql' | Where-Object {
        $_.Name -notlike '*.down.sql' -and [int]$_.Name.Substring(0,3) -le 55
    } | Sort-Object Name)
    if ($migrations.Count -ne 55) { throw 'field visibility verifier expects migrations 001 through 055' }
    foreach ($migration in $migrations) {
        Sql $freshDatabase @('-f',"/migrations/$($migration.Name)") | Out-Null
        if ([int]$migration.Name.Substring(0,3) -le 54) {
            Sql $database @('-f',"/migrations/$($migration.Name)") | Out-Null
            if ([int]$migration.Name.Substring(0,3) -eq 29) {
                foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql')) { Sql $database @('-f',"/dev-seeds/$seed") | Out-Null }
            }
            if ([int]$migration.Name.Substring(0,3) -eq 33) { Sql $database @('-f','/dev-seeds/003_community_social.sql') | Out-Null }
        }
    }
    # A real old 054 private row/revision is retained through 055 and down/reapply.
    $seededOriginal=Snapshot $database
    $ownedOwner=[Guid]::NewGuid().ToString()
    $ownedAgent=[Guid]::NewGuid().ToString()
    $sourceSetup=@'
INSERT INTO accounts(id,account_type) VALUES(:'ownedOwner'::uuid,'person');
INSERT INTO user_profiles(account_id,display_name,bio,visibility)
VALUES(:'ownedOwner'::uuid,'合成旧普通资料','迁移前资料不复制','public');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES(:'ownedAgent'::uuid,'personal',:'ownedOwner'::uuid);
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'ownedAgent'::uuid AND profile_version=1;
INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version)
VALUES(:'ownedAgent'::uuid,:'ownedOwner'::uuid,
    '{"personalPreferences":["合成旧偏好"],"socialPreferences":[],"availability":"合成时间说明",
      "preferredActivityTypes":[],"travelPreferences":[],"interactionPreferences":[],
      "privateCityHistory":"合成历史","languagePreferences":["中文"],"agentNotes":"合成迁移保护内容"}'::jsonb,2);
'@
    SqlText $database $sourceSetup @('-v',"ownedOwner=$ownedOwner",'-v',"ownedAgent=$ownedAgent") | Out-Null
    $before=Snapshot $database
    Sql $database @('-f','/migrations/055_agent_profile_field_visibility.sql') | Out-Null
    if ($before -ne (Snapshot $database)) { throw '055 changed existing complete source rows or private/native revisions' }
    AssertNoPolicies $database
    AssertNoPolicies $freshDatabase
    $proof.seededMigrationPreservesFullRows=$true
    $proof.preMigrationPrivateRevision=2
    Write-Output '[PASS] fresh 001-055 and seeded 001-054 to 055; all source rows/revisions/private content unchanged, rules zero'

    foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql','003_community_social.sql')) { Sql $freshDatabase @('-f',"/dev-seeds/$seed") | Out-Null }
    $bootstrap=(Sql $freshDatabase @('-qAtc',"SELECT
        (SELECT count(*) FROM agent_profiles)=(SELECT count(*) FROM agents WHERE agent_type IN ('personal','organization'))
        AND NOT EXISTS(SELECT 1 FROM agent_private_profiles)
        AND NOT EXISTS(SELECT 1 FROM agent_profile_field_visibility)
        AND NOT EXISTS(SELECT 1 FROM agents WHERE agent_type='business')")).Trim()
    if ($bootstrap -ne 't') { throw 'post-055 seed bootstrap or default private authority failed' }
    $freshBefore=Snapshot $freshDatabase
    $proof.freshAfterMigrationSeedBootstrap=$true
    $fixture=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixtures\agent_profile_visibility_constraints.sql') -Raw
    foreach ($target in @($database,$freshDatabase)) {
        SqlText $target $fixture | Out-Null
        AssertNoPolicies $target
    }
    if ($before -ne (Snapshot $database) -or $freshBefore -ne (Snapshot $freshDatabase)) { throw 'owned DDL/helper fixture changed an existing complete source row' }
    $proof.rawConstraintAndHelperFixturesPass=$true
    Write-Output '[PASS] two databases: complete policy binding/rules/current Community; helper default/each audience/friend/member/Block/withdrawal/inactive/deletion'

    if (!$SkipFullGo) {
        $env:BIRDTIE_DATABASE_URL="postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
        $env:BIRDTIE_DISPOSABLE_DB='1'
        & go test -json ./... -count=1 2>&1 | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-full-go.jsonl")
        $proof.fullGoExit=$LASTEXITCODE
        if ($LASTEXITCODE -ne 0) { throw '055 default-parallel full Go/API/DB compatibility failed' }
        $afterFullGo=Snapshot $database
        if ($before -ne $afterFullGo) {
            SaveSnapshotDifference $before $afterFullGo 'full-go-snapshot'
            throw '055 full Go regression changed complete seed/private source rows'
        }
        AssertNoPolicies $database
        Write-Output '[PASS] 055 default-parallel full Go API/DB compatibility and complete source preservation'
    }

    $policySetup=@'
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'ownedAgent'::uuid AND profile_version=2;
INSERT INTO agent_profile_field_visibility(agent_id,owner_id,rules,written_profile_version)
SELECT :'ownedAgent'::uuid,:'ownedOwner'::uuid,jsonb_object_agg(key,jsonb_build_object('visibility','PRIVATE','communityIds','[]'::jsonb)),3
FROM unnest(ARRAY['displayName','bio','personalPreferences','socialPreferences','availability','preferredActivityTypes',
    'travelPreferences','interactionPreferences','privateCityHistory','languagePreferences','agentNotes']) key;
'@
    SqlText $database $policySetup @('-v',"ownedOwner=$ownedOwner",'-v',"ownedAgent=$ownedAgent") | Out-Null
    $privateThenPolicy=(Sql $database @('-qAtc',"SELECT private.written_profile_version=2 AND a.profile_version=3
        AND private.fields->>'agentNotes'='合成迁移保护内容' FROM agent_private_profiles private
        JOIN agent_profiles a USING(agent_id) WHERE a.agent_id='$ownedAgent'::uuid AND a.owner_id='$ownedOwner'::uuid")).Trim()
    if ($privateThenPolicy -ne 't') { throw 'private revision two then policy CAS revision three was not preserved' }
    $protectedBefore=Snapshot $database -IncludePolicy
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/055_agent_profile_field_visibility.down.sql *> (Join-Path $outputDirectory "$EvidencePrefix-protected-down.log")
    $proof.protectedDownExit=$LASTEXITCODE
    if ($LASTEXITCODE -eq 0) { throw '055 down erased persisted field visibility rules' }
    if ($protectedBefore -ne (Snapshot $database -IncludePolicy)) { throw 'protected 055 down changed policy/private/revision or source rows' }
    $proof.nonemptyDownPreservesExactRows=$true
    Write-Output '[PASS] nonempty policy down rejects atomically; complete rules/private/source contents and versions unchanged'
    SqlText $database @'
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'ownedAgent'::uuid AND profile_version=3;
DELETE FROM agent_profile_field_visibility WHERE agent_id=:'ownedAgent'::uuid;
'@ @('-v',"ownedAgent=$ownedAgent") | Out-Null
    foreach ($target in @($database,$freshDatabase)) {
        AssertNoPolicies $target
        $downBefore=Snapshot $target
        Sql $target @('-f','/migrations/055_agent_profile_field_visibility.down.sql') | Out-Null
        if ($downBefore -ne (Snapshot $target)) { throw 'empty 055 down changed ordinary/private/source rows or metadata version' }
        Sql $target @('-f','/migrations/055_agent_profile_field_visibility.sql') | Out-Null
        if ($downBefore -ne (Snapshot $target)) { throw '055 reapply changed ordinary/private/source rows or metadata version' }
        AssertNoPolicies $target
    }
    $retained=(Sql $database @('-qAtc',"SELECT p.profile_version=4 AND private.written_profile_version=2
        AND private.fields->>'agentNotes'='合成迁移保护内容'
        AND NOT birdtie_agent_profile_field_allowed(p.owner_id,NULL,'agentNotes')
        FROM agent_profiles p JOIN agent_private_profiles private USING(agent_id) WHERE p.agent_id='$ownedAgent'::uuid")).Trim()
    if ($retained -ne 't') { throw 'policy clear/down/reapply reset native/private revision or made private notes public' }
    SqlText $database @'
DELETE FROM accounts WHERE id=:'ownedOwner'::uuid;
'@ @('-v',"ownedOwner=$ownedOwner") | Out-Null
    if ($seededOriginal -ne (Snapshot $database) -or $freshBefore -ne (Snapshot $freshDatabase)) { throw 'down/reapply retained owned source fixture or changed complete seed rows' }
    $proof.emptyDownReapplyPreservesPrivateAndPublicRows=$true
    $proof.privateThenPolicyCASRevisions=@(2,3,4)
    $proof.migrationsThrough=55
    $proof.developmentSeeds=3
    Write-Output '[PASS] explicit policy clear/empty down/reapply preserves complete sources/private v2/native v4; Private never publicly revives'
} catch {
    $proof.failure=$_.Exception.Message
    throw
} finally {
    $env:BIRDTIE_DATABASE_URL=$previousDsn
    $env:BIRDTIE_DISPOSABLE_DB=$previousDisposable
    try {
        foreach ($target in $createdDatabases) {
            if ($target -notmatch '^birdtie_visibility_verify_[0-9a-f]{12}(_fresh)?$') { throw 'invalid disposable visibility database cleanup target' }
            Sql 'postgres' @('-c',"DROP DATABASE $target WITH (FORCE)") | Out-Null
            if ((Sql 'postgres' @('-qAtc',"SELECT count(*) FROM pg_database WHERE datname='$target'")).Trim() -ne '0') { throw 'owned visibility database was not removed' }
        }
        $proof.databasesDropped=$true
        $proof | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-result.json")
        Write-Output '[PASS] only owned visibility migration databases removed; session environment restored'
    } finally { Pop-Location }
}
