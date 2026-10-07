param([switch]$IncludeDetails)

$ErrorActionPreference='Stop'
$query=if ($IncludeDetails) {
    "SELECT id,target_type,target_id,reason,status,created_at,details FROM incident_reports WHERE status IN ('open','reviewing') ORDER BY created_at,id LIMIT 50;"
} else {
    "SELECT id,target_type,target_id,reason,status,created_at FROM incident_reports WHERE status IN ('open','reviewing') ORDER BY created_at,id LIMIT 50;"
}
Push-Location (Join-Path $PSScriptRoot '..\apps\api')
try {
    & docker compose exec -T db psql -U birdtie -d birdtie -v ON_ERROR_STOP=1 -c $query
    if ($LASTEXITCODE -ne 0) { throw 'Could not inspect the local pilot report queue.' }
} finally { Pop-Location }
