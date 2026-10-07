param([string]$EvidencePrefix='production-round1',[ValidateRange(0,3)][int]$FullGoRounds=3)
$ErrorActionPreference='Stop'
if ($EvidencePrefix -notmatch '^[a-z0-9][a-z0-9-]+$') { throw 'invalid Memory DDL evidence prefix' }
$memoryApiDirectory=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\apps\api')).Path
$memoryScopeDatabase='birdtie_memory_scope_'+[Guid]::NewGuid().ToString('N').Substring(0,12)
$memoryEvidenceDirectory=[System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\work\v5-age004'))
[System.IO.Directory]::CreateDirectory($memoryEvidenceDirectory)|Out-Null
if(Test-Path -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-result.json")){throw 'choose a fresh evidence label'}
$memoryScopeCreated=$false
$memoryProof=@{evidenceClass='LOCAL_DISPOSABLE_SYNTHETIC_ONLY';database=$memoryScopeDatabase;stagingOnly=$false}
$memoryNativeOwner=[Guid]::NewGuid().ToString()
$memoryNativeAgent=[Guid]::NewGuid().ToString()

function MemorySql([string]$target,[string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "Memory DDL scope SQL failed ($LASTEXITCODE)" }
}
function MemorySqlText([string]$target,[string]$query,[string[]]$arguments=@()) {
    $query | & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "Memory DDL scope SQL text failed ($LASTEXITCODE)" }
}
function MemorySnapshot([string]$target,[switch]$IncludeMemory) {
    $query=@'
CREATE FUNCTION pg_temp.memory_source_snapshot(include_memory boolean) RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE row record; contents jsonb; result jsonb:='{}'::jsonb;
BEGIN
    FOR row IN SELECT tablename FROM pg_tables WHERE schemaname='public'
        AND (include_memory OR tablename<>'agent_memories') ORDER BY tablename LOOP
        EXECUTE format('SELECT coalesce(jsonb_agg(content ORDER BY content::text),''[]''::jsonb) FROM (SELECT to_jsonb(x) AS content FROM public.%I x) rows',row.tablename)
            INTO contents;
        result:=result||jsonb_build_object(row.tablename,contents);
    END LOOP;
    RETURN result;
END $$;
SELECT pg_temp.memory_source_snapshot(:'includeMemory'::boolean)::text;
'@
    return ((MemorySqlText $target $query @('-qAt','-v',"includeMemory=$([bool]$IncludeMemory)")) -join "`n").Trim()
}

function MemoryCodeSnapshot {
    $files=@(Get-ChildItem -LiteralPath $memoryApiDirectory -Recurse -File -Filter '*.go')+
        @(Get-ChildItem -LiteralPath (Join-Path $memoryApiDirectory 'migrations') -File -Filter '*.sql')+
        @(Get-Item (Join-Path $memoryApiDirectory 'go.mod'),(Join-Path $memoryApiDirectory 'go.sum'))
    return (@($files | Sort-Object FullName | ForEach-Object {
        @{path=$_.FullName.Substring($memoryApiDirectory.Length+1).Replace('\','/');
          sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash}
    }) | ConvertTo-Json -Depth 4)
}

Push-Location -LiteralPath $memoryApiDirectory
try {
    MemorySql 'postgres' @('-c',"CREATE DATABASE $memoryScopeDatabase") | Out-Null
    $memoryScopeCreated=$true
    $memoryMigrations=@(Get-ChildItem -LiteralPath migrations -Filter '*.sql' | Where-Object {
        $_.Name -notlike '*.down.sql' -and [int]$_.Name.Substring(0,3) -le 55
    } | Sort-Object Name)
    if ($memoryMigrations.Count -ne 55) { throw 'Memory scope requires exact migration sequence 001-055' }
    foreach ($migration in $memoryMigrations) {
        MemorySql $memoryScopeDatabase @('-f',"/migrations/$($migration.Name)") | Out-Null
        if ([int]$migration.Name.Substring(0,3) -eq 29) {
            foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql')) {
                MemorySql $memoryScopeDatabase @('-f',"/dev-seeds/$seed") | Out-Null
            }
        }
        if ([int]$migration.Name.Substring(0,3) -eq 33) {
            MemorySql $memoryScopeDatabase @('-f','/dev-seeds/003_community_social.sql') | Out-Null
        }
    }
    $memoryProof.migrationsThrough=55
    $memoryProof.seeds=3
    $memoryOriginalSnapshot=MemorySnapshot $memoryScopeDatabase
    $memoryOriginalSnapshot | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-initial-source.json")
    $nativeSetup=@'
INSERT INTO accounts(id,account_type) VALUES(:'nativeOwner'::uuid,'person');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES(:'nativeAgent'::uuid,'personal',:'nativeOwner'::uuid);
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'nativeAgent'::uuid AND profile_version=1;
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'nativeAgent'::uuid AND profile_version=2;
SELECT profile_version=3 FROM agent_profiles WHERE agent_id=:'nativeAgent'::uuid;
'@
    if (((MemorySqlText $memoryScopeDatabase $nativeSetup @('-qAt','-v',"nativeOwner=$memoryNativeOwner",'-v',"nativeAgent=$memoryNativeAgent")) -join "`n").Trim() -ne 't') {
        throw 'owned native revision preservation fixture failed'
    }
    $memoryBefore=MemorySnapshot $memoryScopeDatabase
    $memoryUp=Get-Content -LiteralPath (Join-Path $memoryApiDirectory 'migrations\056_agent_memory.sql') -Raw
    $memoryDown=Get-Content -LiteralPath (Join-Path $memoryApiDirectory 'migrations\056_agent_memory.down.sql') -Raw
    $memoryFixture=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixtures\agent_memory_constraints.sql') -Raw
    MemorySqlText $memoryScopeDatabase $memoryUp | Tee-Object -FilePath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-up.log") | Out-Null
    if ($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)) { throw 'production056 changed old full source rows' }
    if ((MemorySql $memoryScopeDatabase @('-qAtc','SELECT count(*) FROM agent_memories')).Trim() -ne '0') { throw 'Memory migration backfilled data' }
    $memoryProof.emptyUpPreservesAllSourceRows=$true
    $savedMemoryDsn=$env:BIRDTIE_DATABASE_URL
    $savedMemoryDisposable=$env:BIRDTIE_DISPOSABLE_DB
    $savedMemoryFlags=$env:BIRDTIE_AGENT_FEATURE_FLAGS
    $env:BIRDTIE_DATABASE_URL="postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$memoryScopeDatabase`?sslmode=disable"
    $env:BIRDTIE_DISPOSABLE_DB='1'
    Remove-Item Env:BIRDTIE_AGENT_FEATURE_FLAGS -ErrorAction SilentlyContinue
    try {
        $memoryCodeBefore=MemoryCodeSnapshot
        $memoryCodeBefore | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-code-before.json")
        & go test -json ./internal/agentmemory ./internal/httpapi ./internal/postgres -run 'Test(AgentMemory|Memory)' -count=1 *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-scope.jsonl")
        $memoryProof.scopeExit=$LASTEXITCODE
        if($memoryProof.scopeExit -ne 0){throw 'actual native Memory/domain/HTTP tests failed; original log retained'}
        $memoryProof.fullGoExits=@()
        for($memoryRound=1;$memoryRound -le $FullGoRounds;$memoryRound++){
            & go test -json ./... -count=1 *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-full-$memoryRound.jsonl")
            $memoryProof.fullGoExits+=@($LASTEXITCODE)
            if($LASTEXITCODE -ne 0){throw "full Go Memory regression round $memoryRound failed"}
        }
        & go vet ./... *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-vet.log")
        $memoryProof.vetExit=$LASTEXITCODE
        if($LASTEXITCODE -ne 0){throw 'full Go vet failed'}
        & go build ./... *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-build.log")
        $memoryProof.buildExit=$LASTEXITCODE
        if($LASTEXITCODE -ne 0){throw 'full API build failed'}
        $memoryProbe=Join-Path $memoryEvidenceDirectory "$EvidencePrefix-api.exe"
        & go build -o $memoryProbe . *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-probe-build.log")
        if($LASTEXITCODE -ne 0){throw 'native API binary build failed'}
        $memoryProof.apiBinarySHA256=(Get-FileHash -LiteralPath $memoryProbe -Algorithm SHA256).Hash
        $memoryPort=16996
        if(Get-NetTCPConnection -LocalPort $memoryPort -State Listen -ErrorAction SilentlyContinue){throw 'owned runtime probe port occupied'}
        $savedMemoryAddress=$env:BIRDTIE_API_ADDR
        $savedMemoryDevPhone=$env:BIRDTIE_DEV_PHONE_AUTH
        $env:BIRDTIE_API_ADDR="127.0.0.1:$memoryPort"
        $env:BIRDTIE_DEV_PHONE_AUTH='false'
        $memoryProcess=$null
        try {
            $memoryProcess=Start-Process -FilePath $memoryProbe -WorkingDirectory $memoryApiDirectory -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-runtime.out.log") -RedirectStandardError (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-runtime.err.log")
            $memoryReady=$false
            for($attempt=0;$attempt -lt 40;$attempt++){
                try {
                    $response=Invoke-WebRequest -Uri "http://127.0.0.1:$memoryPort/readyz" -TimeoutSec 1
                    if($response.StatusCode -eq 200){$memoryReady=$true;break}
                } catch { }
                Start-Sleep -Milliseconds 200
            }
            if(!$memoryReady){throw 'current API runtime did not become ready'}
            $memoryObservations=@()
            foreach($probe in @(
                @{method='GET';path='/v1/me/agent-memories'},
                @{method='PUT';path='/v1/me/agent-memories/80000000-0000-4000-8000-000000000011'},
                @{method='DELETE';path='/v1/me/agent-memories/80000000-0000-4000-8000-000000000011'}
            )){
                $response=Invoke-WebRequest -Method $probe.method -Uri "http://127.0.0.1:$memoryPort$($probe.path)" -SkipHttpErrorCheck
                if($response.StatusCode -ne 401 -or $response.Headers['Cache-Control'] -notcontains 'no-store'){throw 'compiled Memory gateway did not deny anonymous without caching'}
                $memoryObservations+=@{method=$probe.method;path=$probe.path;status=[int]$response.StatusCode;requestId=@($response.Headers['X-Request-ID'])}
            }
            $memoryObservations | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-native-runtime.json")
            $memoryProof.apiReadyStatus=200
            $memoryProof.threeNativeMemoryRoutesAnonymous401=$true
        } finally {
            if($memoryProcess -and !$memoryProcess.HasExited){
                $ownedMemoryProcess=Get-Process -Id $memoryProcess.Id -ErrorAction SilentlyContinue
                if($ownedMemoryProcess -and $ownedMemoryProcess.Path -eq [System.IO.Path]::GetFullPath($memoryProbe)){
                    Stop-Process -Id $memoryProcess.Id
                    $ownedMemoryProcess.WaitForExit(10000)
                    $memoryProof.ownedRuntimeProcessStopped=$true
                } else {throw 'owned runtime path mismatch; preserve unknown process and report'}
            }
            $env:BIRDTIE_API_ADDR=$savedMemoryAddress
            $env:BIRDTIE_DEV_PHONE_AUTH=$savedMemoryDevPhone
        }
        $memoryCodeAfter=MemoryCodeSnapshot
        $memoryCodeAfter | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-code-after.json")
        $memoryProof.entireApiSourceHashesStable=($memoryCodeBefore -eq $memoryCodeAfter)
        if(!$memoryProof.entireApiSourceHashesStable){throw 'API source changed during verification; rerun a stable current window'}
    } finally {
        $env:BIRDTIE_DATABASE_URL=$savedMemoryDsn
        $env:BIRDTIE_DISPOSABLE_DB=$savedMemoryDisposable
        $env:BIRDTIE_AGENT_FEATURE_FLAGS=$savedMemoryFlags
    }
    if($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)){throw 'native Memory tests changed old complete source rows'}
    if((MemorySql $memoryScopeDatabase @('-qAtc','SELECT count(*) FROM agent_memories')).Trim() -ne '0'){throw 'native Memory tests left owned records'}
    $memoryProof.goTestsRestoreAllSourceRows=$true
    MemorySqlText $memoryScopeDatabase $memoryFixture | Tee-Object -FilePath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-fixture.log")
    if ($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)) { throw 'strict raw Memory fixture changed old full source rows' }
    if ((MemorySql $memoryScopeDatabase @('-qAtc','SELECT count(*) FROM agent_memories')).Trim() -ne '0') { throw 'strict raw fixture left memory rows' }
    $memoryProof.constraintFixturePass=$true
    $memoryProof.fixtureCleanupExactSourceSnapshotEqual=$true

    $memoryProtectedOwner=[Guid]::NewGuid().ToString()
    $memoryProtectedAgent=[Guid]::NewGuid().ToString()
    $memoryProtectedID=[Guid]::NewGuid().ToString()
    $memoryProtectedSetup=@'
INSERT INTO accounts(id,account_type) VALUES(:'protectedOwner'::uuid,'person');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES(:'protectedAgent'::uuid,'personal',:'protectedOwner'::uuid);
INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,valid_until)
VALUES(:'protectedMemory'::uuid,:'protectedAgent'::uuid,:'protectedOwner'::uuid,'PREFERENCE','protected.down',
    '合成Memory回退保护；不作真实推断证据','{"declared":true}'::jsonb,clock_timestamp()+interval '1 day');
'@
    MemorySqlText $memoryScopeDatabase $memoryProtectedSetup @('-v',"protectedOwner=$memoryProtectedOwner",'-v',"protectedAgent=$memoryProtectedAgent",'-v',"protectedMemory=$memoryProtectedID") | Out-Null
    $memoryProof.protectedDown=@()
    foreach ($state in @('ACTIVE','EXPIRED','DELETED')) {
        if ($state -eq 'EXPIRED') {
            $expire=@'
UPDATE agent_memories SET version=2,status='EXPIRED' WHERE id=:'protectedMemory'::uuid AND version=1;
'@
            MemorySqlText $memoryScopeDatabase $expire @('-v',"protectedMemory=$memoryProtectedID") | Out-Null
        } elseif ($state -eq 'DELETED') {
            $tombstone=@'
UPDATE agent_memories SET version=3,status='DELETED',summary='',structured_value='{}',last_reinforced_at=NULL
WHERE id=:'protectedMemory'::uuid AND version=2;
'@
            MemorySqlText $memoryScopeDatabase $tombstone @('-v',"protectedMemory=$memoryProtectedID") | Out-Null
        }
        $memoryProtectedBefore=MemorySnapshot $memoryScopeDatabase -IncludeMemory
        $memoryDown | & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $memoryScopeDatabase *> (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-down-$($state.ToLower()).log")
        $downExit=$LASTEXITCODE
        if ($downExit -ne 3) { throw "nonempty $state down did not reject with actual SQL error" }
        if ($memoryProtectedBefore -ne (MemorySnapshot $memoryScopeDatabase -IncludeMemory)) { throw "protected $state down changed contents/versions/source rows" }
        $memoryProof.protectedDown+=@(@{state=$state;exit=$downExit;exactSnapshotEqual=$true})
    }
    $protectedDispose=@'
DELETE FROM accounts WHERE id=:'protectedOwner'::uuid;
'@
    MemorySqlText $memoryScopeDatabase $protectedDispose @('-v',"protectedOwner=$memoryProtectedOwner") | Out-Null
    if ($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)) { throw 'owned protected fixture cleanup changed source rows' }
    if ((MemorySql $memoryScopeDatabase @('-qAtc','SELECT count(*) FROM agent_memories')).Trim() -ne '0') { throw 'actual parent cleanup failed' }
    MemorySqlText $memoryScopeDatabase $memoryDown | Tee-Object -FilePath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-down-empty.log") | Out-Null
    if ($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)) { throw 'empty down changed old source/native revision' }
    MemorySqlText $memoryScopeDatabase $memoryUp | Tee-Object -FilePath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-reapply.log") | Out-Null
    if ($memoryBefore -ne (MemorySnapshot $memoryScopeDatabase)) { throw 'reapply changed old source/native revision' }
    $memoryProof.emptyDownReapplyPreservesAllSourceRowsAndNativeV3=$true
    $nativeDispose=@'
DELETE FROM accounts WHERE id=:'nativeOwner'::uuid;
'@
    MemorySqlText $memoryScopeDatabase $nativeDispose @('-v',"nativeOwner=$memoryNativeOwner") | Out-Null
    $memoryFinal=MemorySnapshot $memoryScopeDatabase
    $memoryFinal | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-final-source.json")
    if ($memoryOriginalSnapshot -ne $memoryFinal) { throw 'owned native fixture cleanup did not restore complete old source rows' }
    $memoryProof.finalAllSourceSnapshotEqual=$true
    Write-Output '[PASS] production056 raw constraints, full old source preservation, ACTIVE/EXPIRED/DELETED protected down, empty down/reapply and owned cascade cleanup'
} catch {
    $memoryProof.failure=$_.Exception.Message
    throw
} finally {
    try {
        if ($memoryScopeCreated) {
            if ($memoryScopeDatabase -notmatch '^birdtie_memory_scope_[0-9a-f]{12}$') { throw 'invalid owned Memory scope cleanup target' }
            MemorySql 'postgres' @('-c',"DROP DATABASE $memoryScopeDatabase WITH (FORCE)") | Out-Null
            if ((MemorySql 'postgres' @('-qAtc',"SELECT count(*) FROM pg_database WHERE datname='$memoryScopeDatabase'")).Trim() -ne '0') { throw 'owned Memory scope database was not removed' }
            $memoryProof.databaseDropped=$true
        }
        $memoryProof | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $memoryEvidenceDirectory "$EvidencePrefix-result.json")
    } finally { Pop-Location }
}
