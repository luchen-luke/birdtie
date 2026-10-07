param([switch]$SkipFullGo, [string]$EvidencePrefix='v5-age002-migration-round1')
$ErrorActionPreference='Stop'
if ($EvidencePrefix -notmatch '^v5-age002-migration-[a-z0-9-]+$') { throw 'invalid private migration evidence prefix' }
$apiDirectory=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\apps\api')).Path
$outputDirectory=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\work')).Path
$database='birdtie_private_verify_'+[Guid]::NewGuid().ToString('N').Substring(0,12)
$freshDatabase=$database+'_fresh'
$createdDatabases=@()
$previousDsn=$env:BIRDTIE_DATABASE_URL
$previousDisposable=$env:BIRDTIE_DISPOSABLE_DB
$proof=@{evidenceClass='LOCAL_DISPOSABLE_SYNTHETIC_ONLY';seededDatabase=$database;freshDatabase=$freshDatabase;fullGoRequested=(!$SkipFullGo)}

function Sql([string]$target,[string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "private profile verifier SQL failed ($LASTEXITCODE)" }
}
function SqlText([string]$target,[string]$query,[string[]]$arguments=@()) {
    $query | & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "private profile verifier SQL text failed ($LASTEXITCODE)" }
}
function Snapshot([string]$target,[switch]$IncludePrivate) {
    $query=@'
SELECT jsonb_build_object(
    'accounts',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM accounts x),
    'agents',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agents x),
    'userProfiles',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY account_id),'[]') FROM user_profiles x),
    'metadata',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id),'[]') FROM agent_profiles x),
    'activities',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM activities x),
    'places',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM places x)
)::text;
'@
    $result=(SqlText $target $query @('-qAt')).Trim()
    if ($IncludePrivate) {
        $private=(Sql $target @('-qAtc',"SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY agent_id)::text,'[]') FROM agent_private_profiles x")).Trim()
        $result+='|'+$private
    }
    return $result
}
function AssertNoPrivateRows([string]$target) {
    if ((Sql $target @('-qAtc','SELECT count(*) FROM agent_private_profiles')).Trim() -ne '0') { throw 'migration copied or retained private fields' }
}

Push-Location -LiteralPath $apiDirectory
try {
    foreach ($target in @($database,$freshDatabase)) {
        Sql 'postgres' @('-c',"CREATE DATABASE $target") | Out-Null
        $createdDatabases+=@($target)
    }
    $migrations=@(Get-ChildItem -LiteralPath migrations -Filter '*.sql' | Where-Object {
        $_.Name -notlike '*.down.sql' -and [int]$_.Name.Substring(0,3) -le 54
    } | Sort-Object Name)
    if ($migrations.Count -ne 54) { throw 'private verifier expects exactly migrations 001 through 054' }
    foreach ($migration in $migrations) {
        Sql $freshDatabase @('-f',"/migrations/$($migration.Name)") | Out-Null
        if ([int]$migration.Name.Substring(0,3) -le 53) {
            Sql $database @('-f',"/migrations/$($migration.Name)") | Out-Null
            if ([int]$migration.Name.Substring(0,3) -eq 29) {
                foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql')) {
                    Sql $database @('-f',"/dev-seeds/$seed") | Out-Null
                }
            }
            if ([int]$migration.Name.Substring(0,3) -eq 33) { Sql $database @('-f','/dev-seeds/003_community_social.sql') | Out-Null }
        }
    }
    # Retain a non-initial metadata revision through migration, down and reapply.
    SqlText $database @'
UPDATE agent_profiles SET profile_version=profile_version+1
WHERE agent_id='b1700000-0000-4000-8000-000000000003'::uuid AND profile_version=1;
DO $$ BEGIN
    IF NOT EXISTS(SELECT 1 FROM agent_profiles WHERE agent_id='b1700000-0000-4000-8000-000000000003'
        AND profile_version=2) THEN RAISE EXCEPTION 'required stable seed metadata missing'; END IF;
END $$;
'@ | Out-Null
    $before=Snapshot $database
    Sql $database @('-f','/migrations/054_agent_private_profiles.sql') | Out-Null
    if ($before -ne (Snapshot $database)) { throw '054 changed existing Account/Agent/UserProfile/base metadata/Activity/Place contents' }
    AssertNoPrivateRows $database
    AssertNoPrivateRows $freshDatabase
    $proof.seededMigrationPreservesFullRows=$true
    Write-Output '[PASS] fresh 001-054 and seeded 001-053 to 054; complete existing rows/revisions unchanged, no private backfill'

    foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql','003_community_social.sql')) {
        Sql $freshDatabase @('-f',"/dev-seeds/$seed") | Out-Null
    }
    $bootstrap=(Sql $freshDatabase @('-qAtc',"SELECT
        (SELECT count(*) FROM agent_profiles)=(SELECT count(*) FROM agents WHERE agent_type IN ('personal','organization'))
        AND NOT EXISTS(SELECT 1 FROM agent_private_profiles)
        AND NOT EXISTS(SELECT 1 FROM agents WHERE agent_type='business')")).Trim()
    if ($bootstrap -ne 't') { throw 'post-054 three-seed bootstrap or empty private authority failed' }
    $freshBefore=Snapshot $freshDatabase
    $proof.freshAfterMigrationSeedBootstrap=$true
    $fixture=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixtures\agent_private_profile_constraints.sql') -Raw
    foreach ($target in @($database,$freshDatabase)) {
        SqlText $target $fixture | Out-Null
        AssertNoPrivateRows $target
    }
    if ($before -ne (Snapshot $database) -or $freshBefore -ne (Snapshot $freshDatabase)) { throw 'owned DDL constraint fixture changed pre-existing rows' }
    $proof.rawConstraintFixturesPass=$true
    Write-Output '[PASS] two databases: Person-only complete binding, revision CAS, private shape/bounds, immutable owner/creation, clear and cascade'

    if (!$SkipFullGo) {
        $env:BIRDTIE_DATABASE_URL="postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
        $env:BIRDTIE_DISPOSABLE_DB='1'
        & go test -json ./... -count=1 2>&1 | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-full-go.jsonl")
        $proof.fullGoExit=$LASTEXITCODE
        if ($LASTEXITCODE -ne 0) { throw '054 default-parallel full Go/API/DB compatibility failed' }
        if ($before -ne (Snapshot $database)) { throw '054 full Go regression changed existing seed rows/revisions' }
        AssertNoPrivateRows $database
        Write-Output '[PASS] 054 default-parallel full Go API/DB compatibility, complete seed rows/revisions unchanged'
    }

    $ownedOwner=[Guid]::NewGuid().ToString()
    $ownedAgent=[Guid]::NewGuid().ToString()
    $protectedSetup=@'
INSERT INTO accounts(id,account_type) VALUES(:'ownedOwner'::uuid,'person');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES(:'ownedAgent'::uuid,'personal',:'ownedOwner'::uuid);
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'ownedAgent'::uuid AND profile_version=1;
INSERT INTO agent_private_profiles(agent_id,owner_id,fields,written_profile_version)
VALUES(:'ownedAgent'::uuid,:'ownedOwner'::uuid,
    '{"personalPreferences":[],"socialPreferences":[],"availability":"","preferredActivityTypes":[],
      "travelPreferences":[],"interactionPreferences":[],"privateCityHistory":"","languagePreferences":[],
      "agentNotes":"合成迁移回滚保护内容"}'::jsonb,2);
'@
    SqlText $database $protectedSetup @('-v',"ownedOwner=$ownedOwner",'-v',"ownedAgent=$ownedAgent") | Out-Null
    $protectedBefore=Snapshot $database -IncludePrivate
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/054_agent_private_profiles.down.sql *> (Join-Path $outputDirectory "$EvidencePrefix-protected-down.log")
    $proof.protectedDownExit=$LASTEXITCODE
    if ($LASTEXITCODE -eq 0) { throw '054 down erased persisted private content' }
    if ($protectedBefore -ne (Snapshot $database -IncludePrivate)) { throw '054 protected down changed private contents/version/identity rows' }
    $proof.nonemptyDownPreservesExactRows=$true
    Write-Output '[PASS] nonempty private down rejects atomically; all private contents, versions and old rows unchanged'
    # Explicit disposal applies only to our newly created local verification DB.
    $protectedClear=@'
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'ownedAgent'::uuid AND profile_version=2;
DELETE FROM agent_private_profiles WHERE agent_id=:'ownedAgent'::uuid;
'@
    SqlText $database $protectedClear @('-v',"ownedAgent=$ownedAgent") | Out-Null
    foreach ($target in @($database,$freshDatabase)) {
        AssertNoPrivateRows $target
        $downBefore=Snapshot $target
        Sql $target @('-f','/migrations/054_agent_private_profiles.down.sql') | Out-Null
        if ($downBefore -ne (Snapshot $target)) { throw 'empty 054 down changed metadata revisions or public identities/content' }
        Sql $target @('-f','/migrations/054_agent_private_profiles.sql') | Out-Null
        if ($downBefore -ne (Snapshot $target)) { throw '054 reapply changed metadata revisions or public identities/content' }
        AssertNoPrivateRows $target
    }
    $versionPreserved=(Sql $database @('-qAtc',"SELECT profile_version=3 FROM agent_profiles WHERE agent_id='$ownedAgent'::uuid")).Trim()
    if ($versionPreserved -ne 't') { throw 'explicitly cleared private owner metadata revision reset during down/reapply' }
    SqlText $database @'
DELETE FROM accounts WHERE id=:'ownedOwner'::uuid;
'@ @('-v',"ownedOwner=$ownedOwner") | Out-Null
    if ($before -ne (Snapshot $database) -or $freshBefore -ne (Snapshot $freshDatabase)) { throw 'down/reapply retained owned fixture or changed complete seed rows' }
    $proof.emptyDownReapplyPreservesMetadataAndPublicRows=$true
    $proof.migrationsThrough=54
    $proof.developmentSeeds=3
    Write-Output '[PASS] explicitly empty disposable down/reapply preserves metadata versions and complete Account/Agent/UserProfile/Activity/Place rows'
} catch {
    $proof.failure=$_.Exception.Message
    throw
} finally {
    $env:BIRDTIE_DATABASE_URL=$previousDsn
    $env:BIRDTIE_DISPOSABLE_DB=$previousDisposable
    try {
        foreach ($target in $createdDatabases) {
            if ($target -notmatch '^birdtie_private_verify_[0-9a-f]{12}(_fresh)?$') { throw 'invalid disposable private migration cleanup target' }
            Sql 'postgres' @('-c',"DROP DATABASE $target WITH (FORCE)") | Out-Null
            if ((Sql 'postgres' @('-qAtc',"SELECT count(*) FROM pg_database WHERE datname='$target'")).Trim() -ne '0') { throw 'owned private migration database was not removed' }
        }
        $proof.databasesDropped=$true
        $proof | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $outputDirectory "$EvidencePrefix-result.json")
        Write-Output '[PASS] only owned migration databases removed; session environment restored'
    } finally { Pop-Location }
}
