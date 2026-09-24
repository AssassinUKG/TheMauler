param(
    [switch]$Run,
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

function Invoke-Checked {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Command,
        [Parameter(ValueFromRemainingArguments = $true)]
        [string[]]$Arguments
    )
    & $Command @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Command failed with exit code $LASTEXITCODE"
    }
}

if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    throw "wails CLI was not found on PATH. Install Wails or open a shell where wails is available."
}

if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    Write-Warning "ffmpeg was not found on PATH. Voice-note output needs ffmpeg for OGG/Opus conversion. Run .\setup.ps1 for audio dependency checks."
}

$pythonCmd = Get-Command python -ErrorAction SilentlyContinue
if (-not $pythonCmd) { $pythonCmd = Get-Command py -ErrorAction SilentlyContinue }
if ($pythonCmd) {
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    & $pythonCmd.Source -c "import kokoro, soundfile" *> $null
    $pythonModuleExit = $LASTEXITCODE
    & $pythonCmd.Source -c "import whisper" *> $null
    $pythonWhisperExit = $LASTEXITCODE
    $ErrorActionPreference = $prevEAP
    if ($pythonModuleExit -ne 0) {
        Write-Warning "Kokoro Python packages were not detected. Run: $($pythonCmd.Source) -m pip install kokoro soundfile"
    }
    if ($pythonWhisperExit -ne 0 -and -not (Get-Command whisper -ErrorAction SilentlyContinue)) {
        Write-Warning "Whisper STT was not detected. Voice input needs Whisper with a numba-compatible NumPy. Run: $($pythonCmd.Source) -m pip install 'numpy<2.4' openai-whisper"
    }
} else {
    Write-Warning "Python was not found. Kokoro TTS and Whisper STT need Python packages. Run .\setup.ps1 for setup help."
}

Get-Process -Name TheMauler -ErrorAction SilentlyContinue | Stop-Process -Force

if (-not $SkipTests) {
    Invoke-Checked go test ./...
    Invoke-Checked go vet ./...
}

Push-Location "$Root\frontend"
try {
    Invoke-Checked npm run build
}
finally {
    Pop-Location
}

$plainGoBinary = Join-Path $Root "mauler.exe"
if (Test-Path $plainGoBinary) {
    Remove-Item -LiteralPath $plainGoBinary -Force
}

# Go's optional module index can become stale independently of the source tree
# and report present GOROOT packages as "not in std". Probe the smallest
# standard package and use direct source discovery only for this build when
# that exact local-cache failure is present.
$previousGoDebug = $env:GODEBUG
$stdProbe = & go list unsafe 2>&1
$stdProbeExit = $LASTEXITCODE
if ($stdProbeExit -ne 0 -and (($stdProbe | Out-String) -match "not in std")) {
    Write-Warning "Go module index is stale; building with direct standard-library discovery."
    if ($env:GODEBUG -notmatch '(^|,)goindex=') {
        $env:GODEBUG = if ($env:GODEBUG) { "$env:GODEBUG,goindex=0" } else { "goindex=0" }
    }
}

try {
    Invoke-Checked wails build -clean
}
finally {
    if ($null -eq $previousGoDebug) {
        Remove-Item Env:GODEBUG -ErrorAction SilentlyContinue
    } else {
        $env:GODEBUG = $previousGoDebug
    }
}

$exe = Join-Path $Root "build\bin\TheMauler.exe"
Write-Host "Built $exe"

if ($Run) {
    Start-Process -FilePath $exe
}
