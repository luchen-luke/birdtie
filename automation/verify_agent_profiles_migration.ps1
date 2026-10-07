param([switch]$SkipFullGo)
$ErrorActionPreference = 'Stop'
$apiDirectory = Join-Path $PSScriptRoot '..\apps\api'
$outputDirectory = Join-Path $PSScriptRoot '..\work'
$database = 'birdtie_profile_verify_' + [Guid]::NewGuid().ToString('N').Substring(0,12)
$emptyDatabase = $database + '_empty'
function Sql([string]$target, [string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target @arguments
    if ($LASTEXITCODE -ne 0) { throw "profile verifier SQL failed ($LASTEXITCODE)" }
}
function SqlText([string]$target, [string]$query) {
    $query | & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $target
    if ($LASTEXITCODE -ne 0) { throw "profile verifier SQL text failed ($LASTEXITCODE)" }
}
function IdentitySnapshot([string]$target) {
    (Sql $target @('-qAtc', "SELECT
        (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id)::text,'[]') FROM accounts x)||'|'||
        (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id)::text,'[]') FROM agents x)||'|'||
        (SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY account_id)::text,'[]') FROM user_profiles x)||'|'||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM activities)||'|'||
        (SELECT coalesce(string_agg(id::text,',' ORDER BY id),'') FROM places)")).Trim()
}
Push-Location $apiDirectory
try {
    Sql 'postgres' @('-c', "CREATE DATABASE $database") | Out-Null
    Sql 'postgres' @('-c', "CREATE DATABASE $emptyDatabase") | Out-Null
    $migrations = @(Get-ChildItem migrations -Filter '*.sql' | Where-Object { $_.Name -notlike '*.down.sql' -and [int]$_.Name.Substring(0,3) -le 53 } | Sort-Object Name)
    foreach ($migration in $migrations) {
        Sql $emptyDatabase @('-f', "/migrations/$($migration.Name)") | Out-Null
        if ([int]$migration.Name.Substring(0,3) -le 52) {
            Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
            if ([int]$migration.Name.Substring(0,3) -eq 29) {
                foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql')) {
                    Sql $database @('-f', "/dev-seeds/$seed") | Out-Null
                }
            }
            if ([int]$migration.Name.Substring(0,3) -eq 33) {
                Sql $database @('-f','/dev-seeds/003_community_social.sql') | Out-Null
            }
        }
    }
    $before = IdentitySnapshot $database
    Sql $database @('-f','/migrations/053_agent_profiles.sql') | Out-Null
    if ($before -ne (IdentitySnapshot $database)) { throw '053 changed existing identities, UserProfile or Activity/Place IDs' }
    $backfill = (Sql $database @('-qAtc', "SELECT
        (SELECT count(*) FROM agent_profiles)=(SELECT count(*) FROM agents WHERE agent_type IN ('personal','organization'))
        AND NOT EXISTS(SELECT 1 FROM agent_profiles WHERE profile_version<>1)
        AND NOT EXISTS(SELECT 1 FROM agents WHERE agent_type='business')")).Trim()
    if ($backfill -ne 't') { throw '053 metadata backfill or Business default guard failed' }
    Write-Output '[PASS] 053 empty fresh/up and seeded 052-to-053 preserve old identity/UserProfile/Activity/Place; metadata-only backfill'
    SqlText $database (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'fixtures\agent_profile_constraints.sql') -Raw) | Out-Null
    Write-Output '[PASS] 053 exact owner/type/identity binding, version guards, reserved Business disabled, future Agent bootstrap and cascade'
    foreach ($seed in @('001_badminton.sql','002_functional_mvp.sql','003_community_social.sql')) {
        Sql $emptyDatabase @('-f', "/dev-seeds/$seed") | Out-Null
    }
    $freshBackfill = (Sql $emptyDatabase @('-qAtc', "SELECT
        (SELECT count(*) FROM agent_profiles)=(SELECT count(*) FROM agents WHERE agent_type IN ('personal','organization'))")).Trim()
    if ($freshBackfill -ne 't') { throw 'post-053 seed Agent bootstrap failed' }
    Write-Output '[PASS] fresh 001-053 then all three seeds bootstrap original Agent bindings without new Business Agent'
    $env:BIRDTIE_DATABASE_URL = "postgres://birdtie:birdtie_local_only@127.0.0.1:55432/$database`?sslmode=disable"
    $env:BIRDTIE_DISPOSABLE_DB = '1'
    try {
        if (!$SkipFullGo) {
            & go test -json ./... -count=1 | Set-Content -LiteralPath (Join-Path $outputDirectory 'v5-age001-migration-full-go.jsonl')
            if ($LASTEXITCODE -ne 0) { throw '053 full Go/DB compatibility failed' }
            Write-Output '[PASS] 053 default-parallel full Go API/DB compatibility'
        }
    } finally {
        Remove-Item Env:BIRDTIE_DATABASE_URL -ErrorAction SilentlyContinue
        Remove-Item Env:BIRDTIE_DISPOSABLE_DB -ErrorAction SilentlyContinue
    }
    if ($before -ne (IdentitySnapshot $database)) { throw '053 regression changed seed identities/UserProfile/Activity/Place' }
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database -f /migrations/053_agent_profiles.down.sql *> (Join-Path $outputDirectory 'v5-age001-protected-down.log')
    if ($LASTEXITCODE -eq 0) { throw '053 down erased persisted AgentProfile metadata' }
    $protected = (Sql $database @('-qAtc', "SELECT to_regclass('agent_profiles') IS NOT NULL AND EXISTS(SELECT 1 FROM agent_profiles)")).Trim()
    if ($protected -ne 't') { throw 'failed down changed persisted metadata' }
    Write-Output '[PASS] 053 nonempty protected down rejects atomically and retains metadata'
    # Explicit disposal only in these newly-created local databases; no real data.
    foreach ($target in @($database,$emptyDatabase)) {
        Sql $target @('-c','DELETE FROM agent_profiles') | Out-Null
        Sql $target @('-f','/migrations/053_agent_profiles.down.sql') | Out-Null
        Sql $target @('-f','/migrations/053_agent_profiles.sql') | Out-Null
    }
    if ($before -ne (IdentitySnapshot $database)) { throw '053 empty down/reapply changed identities or old API data' }
    Write-Output '[PASS] 053 empty metadata down/reapply; old identities/UserProfile/Activity/Place preserved'
} finally {
    foreach ($target in @($database,$emptyDatabase)) {
        if ($target -notmatch '^birdtie_profile_verify_[0-9a-f]{12}(_empty)?$') { throw 'invalid disposable database cleanup target' }
        & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d postgres -c "DROP DATABASE IF EXISTS $target WITH (FORCE)" | Out-Null
        if ($LASTEXITCODE -ne 0) { Write-Error 'disposable profile database cleanup failed' }
    }
    Pop-Location
}
