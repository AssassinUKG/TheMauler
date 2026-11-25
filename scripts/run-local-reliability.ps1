param(
    [string]$LocalUrl = 'http://127.0.0.1:8804/v1',
    [ValidateRange(1,5)][int]$Repeats = 5,
    [string]$OutputRoot,
    [string]$GoBinary = 'go'
)

$ErrorActionPreference = 'Stop'
$maulerRepo = Split-Path -Parent $PSScriptRoot
$maulerUri = [Uri]$LocalUrl
if ($maulerUri.Scheme -ne 'http' -or $maulerUri.Host -ne '127.0.0.1' -or $maulerUri.UserInfo -or $maulerUri.Query) {
    throw 'Qualification accepts only an explicit loopback HTTP endpoint.'
}
if (-not $OutputRoot) {
    $OutputRoot = Join-Path $maulerRepo ('build/local-reliability-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
}
$OutputRoot = [IO.Path]::GetFullPath($OutputRoot)
if (Test-Path -LiteralPath $OutputRoot) { throw 'Use fresh output; retained evidence must not be overwritten.' }
$maulerKey = [Environment]::GetEnvironmentVariable('MAULER_LOCAL_EVAL_KEY')
if ([string]::IsNullOrWhiteSpace($maulerKey)) { $maulerKey = [Environment]::GetEnvironmentVariable('INFERENCE_BRIDGE_API_KEY') }
if ([string]::IsNullOrWhiteSpace($maulerKey)) {
    throw 'Supply the existing local credential ephemerally through MAULER_LOCAL_EVAL_KEY or INFERENCE_BRIDGE_API_KEY. This script never copies it into settings.'
}
$maulerProtected = @{}
foreach ($maulerName in @('config.toml','settings.toml','profiles.toml','provider-secrets.json')) {
    $maulerPath = Join-Path ([Environment]::GetFolderPath('UserProfile')) ('.config/mauler/' + $maulerName)
    if (Test-Path -LiteralPath $maulerPath -PathType Leaf) {
        $maulerProtected[$maulerPath] = (Get-FileHash -LiteralPath $maulerPath -Algorithm SHA256).Hash
    }
}
$maulerEnvNames = @('GOPROXY','GOSUMDB','GOTOOLCHAIN','MAULER_LOCAL_EVAL_KEY','MAULER_LOCAL_EVAL_URL','MAULER_LOCAL_EVAL_OUTPUT','MAULER_LOCAL_EVAL_REPEATS')
$maulerPrevious = @{}
foreach ($maulerName in $maulerEnvNames) { $maulerPrevious[$maulerName] = [Environment]::GetEnvironmentVariable($maulerName) }
$maulerExit = 1
try {
    New-Item -ItemType Directory -Path $OutputRoot | Out-Null
    $env:GOPROXY = 'off'
    $env:GOSUMDB = 'off'
    $env:GOTOOLCHAIN = 'auto'
    $env:MAULER_LOCAL_EVAL_KEY = $maulerKey
    $env:MAULER_LOCAL_EVAL_URL = $LocalUrl.TrimEnd('/')
    $env:MAULER_LOCAL_EVAL_OUTPUT = $OutputRoot
    $env:MAULER_LOCAL_EVAL_REPEATS = [string]$Repeats
    Push-Location $maulerRepo
    try {
        & $GoBinary test ./internal/app -run '^TestLocalReliabilityQualification$' -count=1 -v -timeout 30m 2>&1 |
            Tee-Object -FilePath (Join-Path $OutputRoot 'run.log')
        $maulerExit = $LASTEXITCODE
    } finally { Pop-Location }
} finally {
    foreach ($maulerName in $maulerEnvNames) { [Environment]::SetEnvironmentVariable($maulerName,$maulerPrevious[$maulerName],'Process') }
    $maulerUnchanged = $true
    foreach ($maulerPath in $maulerProtected.Keys) {
        $maulerUnchanged = $maulerUnchanged -and (Test-Path -LiteralPath $maulerPath -PathType Leaf) -and
            ((Get-FileHash -LiteralPath $maulerPath -Algorithm SHA256).Hash -eq $maulerProtected[$maulerPath])
    }
    Write-Output ('Production Mauler configuration/credentials byte-for-byte unchanged: ' + $maulerUnchanged)
    if (-not $maulerUnchanged) { $maulerExit = 1 }
    $maulerKey = $null
}
exit $maulerExit

