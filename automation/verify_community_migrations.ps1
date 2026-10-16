param([string]$EvidenceDirectory)
$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_fixture_com_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
$marker = 'BIRDTIE_ISOLATED_FIXTURE_V1:' + $database
$created = $false
$databaseOID = $null

function Sql([string]$db, [string[]]$arguments) {
    if ($db -ne 'postgres' -and $db -ne $database) { throw 'Unowned database target' }
    $fixtureSession = @()
    if ($db -eq $database) {
        $fixtureSession = @('-c', "SET birdtie.fixture_opt_in='isolated-fixtures-v1'; SET birdtie.fixture_database='$database';")
    }
    & docker compose exec -T db psql -q -v ON_ERROR_STOP=1 -U birdtie -d $db @fixtureSession @arguments
    if ($LASTEXITCODE -ne 0) { throw "psql failed in $db ($LASTEXITCODE)" }
}

function DeniedSeed([string]$label, [string[]]$sessionArguments, [string]$reason) {
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $lines = & docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U birdtie -d $database @sessionArguments -f /dev-seeds/001_badminton.sql 2>&1
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previous }
    $text = $lines -join "`n"
    if ($code -eq 0 -or $text -notmatch [regex]::Escape($reason)) { throw "Fixture denial failed: $label" }
    if ($EvidenceDirectory) { $text | Set-Content -LiteralPath (Join-Path $EvidenceDirectory "$label.txt") -Encoding utf8 }
    Write-Output "[PASS] Fixture guard rejects $label before seed rows"
}

Push-Location $api
try {
    $context = (& docker context inspect | ConvertFrom-Json)[0]
    if ($LASTEXITCODE -ne 0 -or $context.Endpoints.docker.Host -notmatch '^(npipe://|unix://)') { throw 'Local Docker context required' }
    if ($EvidenceDirectory) {
        if (!(Test-Path -LiteralPath $EvidenceDirectory)) { New-Item -ItemType Directory -Path $EvidenceDirectory | Out-Null }
    }
    Sql 'postgres' @('-c', "CREATE DATABASE $database") | Out-Null
    $created = $true
    $databaseOID = (Sql 'postgres' @('-Atc', "SELECT oid FROM pg_database WHERE datname='$database'")).Trim()
    DeniedSeed 'unmarked-database' @('-c', "SET birdtie.fixture_opt_in='isolated-fixtures-v1'; SET birdtie.fixture_database='$database';") 'exact owned fixture database marker required'
    Sql 'postgres' @('-c', "COMMENT ON DATABASE $database IS '$marker'") | Out-Null
    DeniedSeed 'missing-opt-in' @() 'explicit fixture session opt-in required'
    DeniedSeed 'mismatched-session-database' @('-c', "SET birdtie.fixture_opt_in='isolated-fixtures-v1'; SET birdtie.fixture_database='birdtie_fixture_other';") 'explicit fixture session opt-in required'
    if ((Sql $database @('-Atc', "SELECT count(*) FROM pg_tables WHERE schemaname='public'")).Trim() -ne '0') { throw 'Denied seed changed empty database' }
    $migrations = @(Get-ChildItem migrations -Filter '*.sql' | Where-Object { $_.Name -notlike '*.down.sql' } | Sort-Object Name)
    foreach ($migration in $migrations | Where-Object { [int]$_.Name.Substring(0, 3) -le 29 }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    Sql $database @('-f', '/dev-seeds/001_badminton.sql') | Out-Null
    Sql $database @('-f', '/dev-seeds/002_functional_mvp.sql') | Out-Null
    $before = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    foreach ($migration in $migrations | Where-Object {
        $number = [int]$_.Name.Substring(0, 3); $number -ge 30 -and $number -le 32
    }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    $after = (Sql $database @('-Atc', 'SELECT count(*) FROM activities')).Trim()
    $organizers = (Sql $database @('-Atc', 'SELECT count(*) FROM activity_organizers')).Trim()
    if ($before -ne $after -or $after -ne $organizers) {
        throw "Activity migration lost rows or organizers: $before/$after/$organizers"
    }
    Write-Output "[PASS] Migration 030-032 forward: existing Activities $before -> $after, organizers $organizers"

    foreach ($number in 32,31,30) {
        $migration = $migrations | Where-Object { [int]$_.Name.Substring(0, 3) -eq $number } | Select-Object -First 1
        $down = $migration.Name.Replace('.sql', '.down.sql')
        Sql $database @('-f', "/migrations/$down") | Out-Null
    }
    foreach ($migration in $migrations | Where-Object {
        $number = [int]$_.Name.Substring(0, 3); $number -ge 30 -and $number -le 32
    }) {
        Sql $database @('-f', "/migrations/$($migration.Name)") | Out-Null
    }
    Write-Output '[PASS] Migration 032/031/030 down and 030/031/032 reapply on disposable seeded database'

    foreach ($file in '030_community_memberships.sql','031_activity_organizers.sql','032_activity_visibility.sql') {
        $content = Get-Content "test-sql/$file" -Raw -Encoding utf8
        $content | docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $database | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "SQL invariant test $file failed" }
    }
    foreach ($run in 1,2) { Sql $database @('-f', '/dev-seeds/003_community_social.sql') | Out-Null }
    $seed = (Sql $database @('-Atc', "SELECT count(*) FROM communities WHERE id IN ('b1700000-0000-4000-8000-000000000030','b1700000-0000-4000-8000-000000000031','b1700000-0000-4000-8000-000000000032')")).Trim()
    if ($seed -ne '3') { throw "Community seed repetition produced $seed rows" }
    Write-Output '[PASS] SQL invariants 030-032 and Community seed applied twice: three fixture Communities'
}
finally {
    try {
        if ($created) {
            $actual = (Sql 'postgres' @('-Atc', "SELECT json_build_object('oid',oid,'marker',shobj_description(oid,'pg_database')) FROM pg_database WHERE datname='$database'") | ConvertFrom-Json)
            if ($database -notmatch '^birdtie_fixture_com_[0-9a-f]{12}$' -or $actual.oid.ToString() -ne $databaseOID -or $actual.marker -ne $marker) { throw 'Disposable database identity changed; refusing drop' }
            Sql 'postgres' @('-c', "DROP DATABASE $database WITH (FORCE)") | Out-Null
            if ((Sql 'postgres' @('-Atc', "SELECT count(*) FROM pg_database WHERE datname='$database'")).Trim() -ne '0') { throw 'Disposable database remains' }
            Write-Output '[PASS] Exact marked disposable fixture database dropped'
        }
    }
    finally { Pop-Location }
}
