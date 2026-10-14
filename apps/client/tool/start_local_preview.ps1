param(
    [ValidateSet('Start', 'Status', 'Stop')][string]$Action = 'Start',
    [string]$ConfigPath = (Join-Path $PSScriptRoot '..\.env.preview.local.json'),
    [string]$PythonPath
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($PythonPath)) {
    $PythonPath = $env:BIRDTIE_PREVIEW_PYTHON
}
if ([string]::IsNullOrWhiteSpace($PythonPath)) {
    $PythonPath = Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe'
}
if (-not (Test-Path -LiteralPath $PythonPath -PathType Leaf)) {
    throw 'Preview Python runtime was not found. Pass -PythonPath with an installed Python 3.11+ executable.'
}
if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
    throw 'Local preview configuration is missing: apps/client/.env.preview.local.json.'
}
& $PythonPath (Join-Path $PSScriptRoot 'local_preview_runtime.py') ($Action.ToLowerInvariant()) '--config' $ConfigPath
if ($LASTEXITCODE -ne 0) {
    throw "Local preview $Action failed. See the safe error code above."
}
