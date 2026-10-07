param([ValidatePattern('^[a-z0-9][a-z0-9-]+$')][string]$EvidencePrefix='final-round1',[ValidateRange(0,3)][int]$FullGoRounds=3,[switch]$RuntimeProbe)
$ErrorActionPreference='Stop'
$evidenceApi=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\apps\api')).Path
$evidenceOut=[System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\work\v5-age005'))
[System.IO.Directory]::CreateDirectory($evidenceOut)|Out-Null
if(Test-Path -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-result.json")){throw 'choose an unused evidence label'}
$evidenceDatabase='birdtie_evidence_verify_'+[Guid]::NewGuid().ToString('N').Substring(0,12)
$evidenceCreated=$false
$proof=@{evidenceClass='LOCAL_DISPOSABLE_SYNTHETIC_ONLY';database=$evidenceDatabase;prefix=$EvidencePrefix;fullGoExits=@()}
$savedDsn=$env:BIRDTIE_DATABASE_URL;$savedDisposable=$env:BIRDTIE_DISPOSABLE_DB;$savedFlags=$env:BIRDTIE_AGENT_FEATURE_FLAGS
function EvidenceSql([string]$db,[string]$sql,[string[]]$sqlArguments=@()){
 $sql|& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $db @sqlArguments
 if($LASTEXITCODE-ne 0){throw "owned Evidence SQL failed exit $LASTEXITCODE"}
}
function EvidenceSnapshot([switch]$AllTables){
 $query=@'
CREATE FUNCTION pg_temp.evidence_snapshot(all_tables boolean) RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE row record; value jsonb; result jsonb:='{}'::jsonb;
BEGIN
 FOR row IN SELECT tablename FROM pg_tables WHERE schemaname='public' AND (all_tables OR tablename<>'agent_memory_evidence') ORDER BY tablename LOOP
   EXECUTE format('SELECT coalesce(jsonb_agg(v ORDER BY v::text),''[]''::jsonb) FROM (SELECT to_jsonb(t) AS v FROM public.%I t) rows',row.tablename) INTO value;
   result:=result||jsonb_build_object(row.tablename,value);
 END LOOP;RETURN result;
END $$;
SELECT pg_temp.evidence_snapshot(:'allTables'::boolean)::text;
'@
 return ((EvidenceSql $evidenceDatabase $query @('-qAt','-v',"allTables=$([bool]$AllTables)"))-join "`n").Trim()
}
function EvidenceCodeSnapshot{
 $files=@(Get-ChildItem -LiteralPath $evidenceApi -Recurse -File -Filter '*.go')+@(Get-ChildItem -LiteralPath (Join-Path $evidenceApi 'migrations') -File -Filter '*.sql')+@(Get-Item (Join-Path $evidenceApi 'go.mod'),(Join-Path $evidenceApi 'go.sum'))
 return (@($files|Sort-Object FullName|ForEach-Object{@{path=$_.FullName.Substring($evidenceApi.Length+1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash}})|ConvertTo-Json -Depth 4)
}
function EvidenceGo([string[]]$goArguments,[string]$name){
 & go @goArguments *> (Join-Path $evidenceOut "$EvidencePrefix-$name")
 $code=$LASTEXITCODE;if($code-ne 0){throw "Go $name failed exit $code; raw log retained"};return $code
}
Push-Location -LiteralPath $evidenceApi
try{
 EvidenceSql 'postgres' "CREATE DATABASE $evidenceDatabase"|Out-Null;$evidenceCreated=$true
 $migrations=@(Get-ChildItem -LiteralPath migrations -File -Filter '*.sql'|Where-Object{$_.Name-notlike '*.down.sql' -and [int]$_.Name.Substring(0,3)-le 56}|Sort-Object Name)
 if($migrations.Count-ne 56){throw 'expected exact001-056 migration sequence'}
 foreach($migration in $migrations){
  EvidenceSql $evidenceDatabase "\i /migrations/$($migration.Name)"|Out-Null
  if([int]$migration.Name.Substring(0,3)-eq 29){foreach($seed in @('001_badminton.sql','002_functional_mvp.sql')){EvidenceSql $evidenceDatabase "\i /dev-seeds/$seed"|Out-Null}}
  if([int]$migration.Name.Substring(0,3)-eq 33){EvidenceSql $evidenceDatabase '\i /dev-seeds/003_community_social.sql'|Out-Null}
 }
 $proof.oldMigration=56;$proof.seeds=3
 $original=EvidenceSnapshot
 $owner=[Guid]::NewGuid().ToString();$agent=[Guid]::NewGuid().ToString();$memory=[Guid]::NewGuid().ToString()
 $nativeArgs=@('-qAt','-v',"nativeOwner=$owner",'-v',"nativeAgent=$agent",'-v',"nativeMemory=$memory")
 $native=@'
INSERT INTO accounts(id,account_type) VALUES(:'nativeOwner'::uuid,'person');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES(:'nativeAgent'::uuid,'personal',:'nativeOwner'::uuid);
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'nativeAgent'::uuid;
UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=:'nativeAgent'::uuid;
INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,valid_until)
 VALUES(:'nativeMemory'::uuid,:'nativeAgent'::uuid,:'nativeOwner'::uuid,'PREFERENCE','native-old-005','本人声明-仅迁移fixture','{}',clock_timestamp()+interval '1 day');
UPDATE agent_memories SET version=version+1,summary='原有本人声明-仅迁移fixture' WHERE id=:'nativeMemory'::uuid;
SELECT profile_version=3 FROM agent_profiles WHERE agent_id=:'nativeAgent'::uuid;
'@
 if(((EvidenceSql $evidenceDatabase $native $nativeArgs)-join "`n").Trim()-ne 't'){throw 'old native metadata v3/Memory v2 fixture failed'}
 $before=EvidenceSnapshot;$before|Set-Content -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-initial-source.json")
 $up=Get-Content -LiteralPath migrations/057_agent_memory_evidence.sql -Raw
 $down=Get-Content -LiteralPath migrations/057_agent_memory_evidence.down.sql -Raw
 EvidenceSql $evidenceDatabase $up|Out-Null
 if($before-cne (EvidenceSnapshot)){throw '057 up changed old complete public source rows'}
 $proof.emptyUpPreservesAllSourceRows=$true
 $allBefore=EvidenceSnapshot -AllTables
 $fixture=Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixtures/agent_memory_evidence_constraints.sql') -Raw
 $fixture|& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $evidenceDatabase @nativeArgs *> (Join-Path $evidenceOut "$EvidencePrefix-sql-fixture.log")
 if($LASTEXITCODE-ne 0){throw 'Evidence strict constraint fixture failed'}
 if($allBefore-cne (EvidenceSnapshot -AllTables)){throw 'constraint fixture failed to rollback all rows'}
 $proof.constraintFixturePass=$true
 $env:BIRDTIE_DATABASE_URL="postgres://birdtie:birdtie_local_only@127.0.0.1:55432/${evidenceDatabase}?sslmode=disable";$env:BIRDTIE_DISPOSABLE_DB='1'
 Remove-Item Env:BIRDTIE_AGENT_FEATURE_FLAGS -ErrorAction SilentlyContinue
 $codeBefore=EvidenceCodeSnapshot;$codeBefore|Set-Content -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-code-before.json")
 $proof.scopeExit=EvidenceGo @('test','-json','./internal/agentmemory','./internal/httpapi','./internal/postgres','-run','Test(AgentMemory|Memory|Evidence)','-count=1') 'scope.jsonl'
 for($round=1;$round-le $FullGoRounds;$round++){$proof.fullGoExits+=@(EvidenceGo @('test','-json','./...','-count=1') "full-$round.jsonl")}
 $proof.vetExit=EvidenceGo @('vet','./...') 'vet.log';$proof.buildExit=EvidenceGo @('build','./...') 'build.log'
 if($RuntimeProbe){
  $evidenceBinary=Join-Path $evidenceOut "$EvidencePrefix-api.exe"
  $proof.probeBuildExit=EvidenceGo @('build','-o',$evidenceBinary,'.') 'probe-build.log'
  $proof.apiBinarySHA256=(Get-FileHash -LiteralPath $evidenceBinary -Algorithm SHA256).Hash
  $evidencePort=16997
  if(Get-NetTCPConnection -LocalPort $evidencePort -State Listen -ErrorAction SilentlyContinue){throw 'owned16997 runtime probe port occupied'}
  $savedProbeAddress=$env:BIRDTIE_API_ADDR;$savedProbePhone=$env:BIRDTIE_DEV_PHONE_AUTH
  $env:BIRDTIE_API_ADDR="127.0.0.1:$evidencePort";$env:BIRDTIE_DEV_PHONE_AUTH='false'
  $evidenceProcess=$null
  try{
   $evidenceProcess=Start-Process -FilePath $evidenceBinary -WorkingDirectory $evidenceApi -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $evidenceOut "$EvidencePrefix-runtime.out.log") -RedirectStandardError (Join-Path $evidenceOut "$EvidencePrefix-runtime.err.log")
   $probeReady=$false
   for($probeAttempt=0;$probeAttempt -lt 40;$probeAttempt++){
    try{if((Invoke-WebRequest -Uri "http://127.0.0.1:$evidencePort/readyz" -TimeoutSec 1).StatusCode -eq 200){$probeReady=$true;break}}catch{}
    Start-Sleep -Milliseconds 200
   }
   if(!$probeReady){throw 'current compiled Evidence API did not become ready'}
   $probeObservations=@()
   foreach($probeRoute in @(
    @{method='GET';path='/v1/me/agent-memories/80000000-0000-4000-8000-000000000011/provenance'},
    @{method='PUT';path='/v1/me/agent-memories/80000000-0000-4000-8000-000000000011/evidence/80000000-0000-4000-8000-000000000012'},
    @{method='DELETE';path='/v1/me/agent-memories/80000000-0000-4000-8000-000000000011/evidence/80000000-0000-4000-8000-000000000012'}
   )){
    $probeResponse=Invoke-WebRequest -Method $probeRoute.method -Uri "http://127.0.0.1:$evidencePort$($probeRoute.path)" -SkipHttpErrorCheck
    if($probeResponse.StatusCode -ne 401 -or $probeResponse.Headers['Cache-Control'] -notcontains 'no-store'){throw 'compiled Evidence route did not deny anonymous with no-store'}
    $probeObservations+=@{method=$probeRoute.method;path=$probeRoute.path;status=[int]$probeResponse.StatusCode;requestId=@($probeResponse.Headers['X-Request-ID'])}
   }
   $probeObservations|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-native-runtime.json")
   $proof.apiReadyStatus=200;$proof.threeEvidenceRoutesAnonymous401=$true
  }finally{
   if($evidenceProcess -and !$evidenceProcess.HasExited){
    $ownedProbeProcess=Get-Process -Id $evidenceProcess.Id -ErrorAction SilentlyContinue
    if($ownedProbeProcess -and $ownedProbeProcess.Path -eq [System.IO.Path]::GetFullPath($evidenceBinary) -and (Get-FileHash -LiteralPath $evidenceBinary -Algorithm SHA256).Hash -eq $proof.apiBinarySHA256){
     Stop-Process -Id $evidenceProcess.Id
     if(!$ownedProbeProcess.WaitForExit(10000)){throw 'owned probe process did not exit'}
     $proof.ownedRuntimeProcessStopped=$true
    }else{throw 'owned runtime process path/SHA mismatch; preserve unrelated process'}
   }else{$proof.ownedRuntimeProcessStopped=$true}
   $env:BIRDTIE_API_ADDR=$savedProbeAddress;$env:BIRDTIE_DEV_PHONE_AUTH=$savedProbePhone
  }
 }
 if($allBefore-cne (EvidenceSnapshot -AllTables)){throw 'scope/fullGo changed complete public rows or leaked owned fixture'}
 $proof.goTestsRestoreAllSourceRows=$true
 $codeAfter=EvidenceCodeSnapshot;$codeAfter|Set-Content -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-code-after.json")
 if($codeBefore-cne $codeAfter){throw 'API source changed during proof window'};$proof.entireApiSourceHashesStable=$true

 # A separate owned probe has no relationship to the original rows. Do not
 # physically delete Evidence behind the guard merely to get a passing down.
 $probeOwner=[Guid]::NewGuid().ToString();$probeAgent=[Guid]::NewGuid().ToString();$probeMemory=[Guid]::NewGuid().ToString()
 $probeEvidence=[Guid]::NewGuid().ToString();$probeSource=[Guid]::NewGuid().ToString()
 $probe=@"
INSERT INTO accounts(id,account_type) VALUES('$probeOwner','person');
INSERT INTO agents(id,agent_type,principal_account_id) VALUES('$probeAgent','personal','$probeOwner');
INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,valid_until)
 VALUES('$probeMemory','$probeAgent','$probeOwner','PREFERENCE','probe005','本地形状fixture不是实际来源','{}',clock_timestamp()+interval '1 day');
INSERT INTO agent_memory_evidence(id,memory_id,memory_version,agent_id,owner_id,source_type,source_id,source_version_kind,source_revision,signal_type,weight,event_time)
 VALUES('$probeEvidence','$probeMemory',1,'$probeAgent','$probeOwner','MOMENT','$probeSource','REVISION',1,'MANUAL_REFERENCE',1,clock_timestamp()-interval '1 second');
"@
 EvidenceSql $evidenceDatabase $probe|Out-Null
 $proof.protectedDown=@()
 foreach($state in @('CURRENT','REMOVED')){
  if($state-eq 'REMOVED'){EvidenceSql $evidenceDatabase "UPDATE agent_memory_evidence SET version=version+1,status='REMOVED',source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL WHERE id='$probeEvidence'"|Out-Null}
  $guardBefore=EvidenceSnapshot -AllTables
  $down|& docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $evidenceDatabase *> (Join-Path $evidenceOut "$EvidencePrefix-protected-$state-down.log")
  $exit=$LASTEXITCODE;$equal=$guardBefore-ceq (EvidenceSnapshot -AllTables)
  $proof.protectedDown+=@(@{status=$state;exit=$exit;exactSnapshotEqual=$equal})
  if($exit-ne 3 -or !$equal){throw 'nonempty Evidence down was not atomically protected'}
 }
 EvidenceSql $evidenceDatabase "DELETE FROM agent_profiles WHERE agent_id='$probeAgent'; DELETE FROM agents WHERE id='$probeAgent'; DELETE FROM accounts WHERE id='$probeOwner';"|Out-Null
 if($allBefore-cne (EvidenceSnapshot -AllTables)){throw 'owned probe cascade modified old public source rows'}
 EvidenceSql $evidenceDatabase $down|Out-Null
 if($before-cne (EvidenceSnapshot)){throw 'empty057 down changed prior native rows'}
 EvidenceSql $evidenceDatabase $up|Out-Null
 if($allBefore-cne (EvidenceSnapshot -AllTables)){throw 'empty057 reapply changed prior native v3/Memory v2'}
 $proof.emptyDownReapplyPreservesAllSourceRowsAndNativeV3MemoryV2=$true
 EvidenceSql $evidenceDatabase "DELETE FROM agent_profiles WHERE agent_id='$agent'; DELETE FROM agents WHERE id='$agent'; DELETE FROM accounts WHERE id='$owner';"|Out-Null
 if($original-cne (EvidenceSnapshot)){throw 'final cleanup did not restore original public rows'}
 $proof.finalAllSourceSnapshotEqual=$true
 Write-Output 'PASS Evidence native scope/fullGo/vet/build/strict constraints/source preservation/down/reapply'
}catch{$proof.failure=$_.Exception.Message;throw}finally{
 $env:BIRDTIE_DATABASE_URL=$savedDsn;$env:BIRDTIE_DISPOSABLE_DB=$savedDisposable;$env:BIRDTIE_AGENT_FEATURE_FLAGS=$savedFlags
 try{if($evidenceCreated){if($evidenceDatabase-notmatch '^birdtie_evidence_verify_[0-9a-f]{12}$'){throw 'invalid owned drop target'};EvidenceSql 'postgres' "DROP DATABASE $evidenceDatabase WITH (FORCE)"|Out-Null;$proof.databaseDropped=$true}}
 finally{$proof|ConvertTo-Json -Depth 9|Set-Content -LiteralPath (Join-Path $evidenceOut "$EvidencePrefix-result.json");Pop-Location}
}
