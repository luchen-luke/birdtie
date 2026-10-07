[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Stop-Safely([string]$Message) {
    [Console]::Error.WriteLine("ERROR: $Message")
    exit 1
}

$target = $env:CIVU_BACKUP_DIR
if ([string]::IsNullOrWhiteSpace($target)) { Stop-Safely 'Set CIVU_BACKUP_DIR to a new directory on encrypted offsite storage.' }
if (-not ($env:PGHOST -or $env:PGSERVICE -or $env:DATABASE_URL)) { Stop-Safely 'Configure PostgreSQL from a protected session/service file; never pass credentials as arguments.' }
if (Test-Path -LiteralPath $target) { Stop-Safely 'Backup target already exists; refusing to overwrite.' }

$commands = @{}
foreach ($name in @('psql', 'pg_dump', 'pg_restore')) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if (-not $cmd) { Stop-Safely "Required PostgreSQL client is missing: $name" }
    $commands[$name] = $cmd.Source
}

New-Item -ItemType Directory -Path $target | Out-Null
try {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $acl = Get-Acl -LiteralPath $target
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($rule in @($acl.Access)) { [void]$acl.RemoveAccessRuleAll($rule) }
    $ownerRule = [Security.AccessControl.FileSystemAccessRule]::new(
        $identity, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
    $acl.SetAccessRule($ownerRule)
    Set-Acl -LiteralPath $target -AclObject $acl
}
catch {
    Remove-Item -LiteralPath $target -Recurse -Force -ErrorAction SilentlyContinue
    Stop-Safely 'Could not restrict backup directory ACL to the current operator.'
}
$dumpTmp = Join-Path $target 'database.dump.partial'
$dump = Join-Path $target 'database.dump'
$manifestTmp = Join-Path $target 'manifest.json.partial'
$manifest = Join-Path $target 'manifest.json'

try {
    $version = & $commands.psql -X -v ON_ERROR_STOP=1 -Atqc 'SHOW server_version'
    if ($LASTEXITCODE -ne 0 -or -not $version) { throw 'Could not read PostgreSQL server version.' }
    $dbName = & $commands.psql -X -v ON_ERROR_STOP=1 -Atqc 'SELECT current_database()'
    if ($LASTEXITCODE -ne 0 -or -not $dbName) { throw 'Could not read database name.' }
    $dbBytesText = & $commands.psql -X -v ON_ERROR_STOP=1 -Atqc 'SELECT pg_database_size(current_database())'
    if ($LASTEXITCODE -ne 0 -or $dbBytesText -notmatch '^\d+$') { throw 'Could not estimate database size.' }
    $dbBytes = [int64]$dbBytesText
    $drive = [IO.Path]::GetPathRoot([IO.Path]::GetFullPath($target))
    $disk = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='$($drive.TrimEnd('\'))'"
    if (-not $disk) { throw 'Could not read backup target free space.' }
    $required = $dbBytes + [math]::Ceiling($dbBytes * 0.25) + 1GB
    if ($disk.FreeSpace -lt $required) { throw 'Insufficient target space: require estimated DB size + 25% + 1 GiB.' }

    Write-Output "Preparing custom-format backup: database=$dbName, server_version=$version, estimated_bytes=$dbBytes"
    & $commands.pg_dump --format=custom --no-owner --no-acl --verbose --file=$dumpTmp
    if ($LASTEXITCODE -ne 0) { throw 'pg_dump failed; partial file will be removed.' }
    if (-not (Test-Path -LiteralPath $dumpTmp) -or (Get-Item -LiteralPath $dumpTmp).Length -eq 0) { throw 'pg_dump output is empty.' }
    & $commands.pg_restore --list $dumpTmp *> $null
    if ($LASTEXITCODE -ne 0) { throw 'pg_restore could not read dump catalog.' }
    Move-Item -LiteralPath $dumpTmp -Destination $dump

    $migrationVersion = & $commands.psql -X -v ON_ERROR_STOP=1 -Atqc "SELECT COALESCE(max(name), 'unknown') FROM civu_schema_migrations" 2>$null
    if ($LASTEXITCODE -ne 0) { $migrationVersion = 'ledger-unavailable' }
    $dumpHash = (Get-FileHash -LiteralPath $dump -Algorithm SHA256).Hash.ToLowerInvariant()
    $record = [ordered]@{
        created_at_utc = [DateTime]::UtcNow.ToString('o')
        database_name = $dbName
        postgres_server_version = $version
        estimated_database_bytes = $dbBytes
        highest_civu_migration = $migrationVersion
        dump_file = [IO.Path]::GetFileName($dump)
        dump_bytes = (Get-Item -LiteralPath $dump).Length
        dump_sha256 = $dumpHash
        format = 'pg_dump custom'
        scope_note = 'Database only. External media files and OSS objects require separate inventory and encrypted copy.'
    }
    $record | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestTmp -Encoding utf8NoBOM
    Move-Item -LiteralPath $manifestTmp -Destination $manifest
    Set-Content -LiteralPath (Join-Path $target 'manifest.sha256') -Value "$dumpHash  database.dump" -Encoding ascii
    if ((Get-FileHash -LiteralPath $dump -Algorithm SHA256).Hash.ToLowerInvariant() -ne $dumpHash) { throw 'Dump checksum verification failed.' }
    Write-Output "Verified local artifacts: $manifest"
    Write-Warning 'This verifies a database dump only. It does not prove an offsite copy or isolated restore.'
}
catch {
    Remove-Item -LiteralPath $dumpTmp, $manifestTmp -Force -ErrorAction SilentlyContinue
    Stop-Safely $_.Exception.Message
}
