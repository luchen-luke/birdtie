$ErrorActionPreference = 'Stop'
$api = Join-Path $PSScriptRoot '..\apps\api'
$database = 'birdtie_com_verify_' + [Guid]::NewGuid().ToString('N').Substring(0, 12)

function Sql([string]$db, [string[]]$arguments) {
    & docker compose exec -T db psql -v ON_ERROR_STOP=1 -U birdtie -d $db @arguments
    if ($LASTEXITCODE -ne 0) { throw "psql failed in $db ($LASTEXITCODE)" }
}

Push-Location $api
try {
    Sql 'postgres' @('-c', "CREATE DATABASE $database") | Out-Null
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
    try { Sql 'postgres' @('-c', "DROP DATABASE IF EXISTS $database WITH (FORCE)") | Out-Null }
    finally { Pop-Location }
}
