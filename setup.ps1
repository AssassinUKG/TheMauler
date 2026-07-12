#Requires -Version 5.1
<#
TheMauler Windows setup and dependency checker.

Use:
  .\setup.ps1 -Check   # report only
  .\setup.ps1 -Auto    # install what can be installed automatically
#>

param(
    [switch]$Check,
    [switch]$Auto
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Ok { param([string]$m) Write-Host "  [OK]  $m" -ForegroundColor Green }
function Write-Warn { param([string]$m) Write-Host "  [!!]  $m" -ForegroundColor Yellow }
function Write-Fail { param([string]$m) Write-Host "  [XX]  $m" -ForegroundColor Red }
function Write-Info { param([string]$m) Write-Host "  [..]  $m" -ForegroundColor Cyan }
function Write-Head { param([string]$m) Write-Host "`n=== $m ===" -ForegroundColor White }

function Test-Command {
    param([string]$Cmd)
    return [bool](Get-Command $Cmd -ErrorAction SilentlyContinue)
}

function Confirm-Install {
    param([string]$Item)
    if ($Auto) { return $true }
    if ($Check) { return $false }
    $resp = Read-Host ("  Install {0}? [Y/n]" -f $Item)
    return ($resp -eq "" -or $resp -match "^[Yy]")
}

function Refresh-Path {
    $env:PATH =
    [Environment]::GetEnvironmentVariable("PATH", "Machine") + ";" +
    [Environment]::GetEnvironmentVariable("PATH", "User")
}

function Install-Winget {
    param([string]$Id, [string]$Name)
    if (-not (Test-Command "winget")) {
        Write-Warn "winget not found - install $Name manually"
        return $false
    }
    Write-Info "Installing $Name via winget ($Id)..."
    winget install --id $Id --silent --accept-package-agreements --accept-source-agreements
    Refresh-Path
    return $true
}

function Find-Python {
    foreach ($cmd in @("python", "py", "python3")) {
        $found = Get-Command $cmd -ErrorAction SilentlyContinue
        if ($found) { return $cmd }
    }
    return $null
}

function Test-PythonModule {
    param([string]$Python, [string]$Module)
    $prevEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    & $Python -c "import $Module" *> $null
    $exit = $LASTEXITCODE
    $ErrorActionPreference = $prevEAP
    return ($exit -eq 0)
}

$issues = [System.Collections.Generic.List[string]]::new()
$actions = [System.Collections.Generic.List[string]]::new()

Write-Head "TheMauler Windows Setup"

Write-Head "Core build tools"
foreach ($dep in @(
    @{ Cmd = "go"; Name = "Go 1.26+"; Winget = "GoLang.Go" },
    @{ Cmd = "node"; Name = "Node.js LTS"; Winget = "OpenJS.NodeJS.LTS" },
    @{ Cmd = "npm"; Name = "npm"; Winget = "" },
    @{ Cmd = "wails"; Name = "Wails CLI"; Winget = "" },
    @{ Cmd = "git"; Name = "Git"; Winget = "Git.Git" }
)) {
    if (Test-Command $dep.Cmd) {
        Write-Ok "$($dep.Name) detected"
        continue
    }
    Write-Warn "$($dep.Name) not found"
    $issues.Add("$($dep.Name) missing")
    if ($dep.Winget -and (Confirm-Install $dep.Name)) {
        if (Install-Winget $dep.Winget $dep.Name) {
            $actions.Add("$($dep.Name) install attempted")
        }
    }
}

if (-not (Test-Command "wails") -and (Test-Command "go") -and (Confirm-Install "Wails CLI via go install")) {
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    Refresh-Path
    if (Test-Command "wails") {
        Write-Ok "Wails CLI installed"
        $actions.Add("Wails CLI installed")
    } else {
        Write-Warn "Wails installed but not on PATH yet; restart terminal or add GOPATH\\bin"
    }
}

Write-Head "Audio / voice runtime"

if (Test-Command "ffmpeg") {
    Write-Ok "ffmpeg detected"
} else {
    Write-Warn "ffmpeg not found - Telegram voice replies cannot be converted to OGG/Opus"
    $issues.Add("ffmpeg missing for voice-note conversion")
    if (Confirm-Install "ffmpeg via winget") {
        Install-Winget "Gyan.FFmpeg" "ffmpeg"
        $actions.Add("ffmpeg install attempted")
    }
}

$python = Find-Python
if ($python) {
    Write-Ok "Python detected ($python)"
} else {
    Write-Warn "Python not found - Kokoro TTS cannot run"
    $issues.Add("Python missing for Kokoro TTS")
    if (Confirm-Install "Python 3.12 via winget") {
        Install-Winget "Python.Python.3.12" "Python 3.12"
        $actions.Add("Python install attempted")
        $python = Find-Python
    }
}

if ($python) {
	$kokoroOk = (Test-PythonModule $python "kokoro")
	$soundfileOk = (Test-PythonModule $python "soundfile")
	$whisperOk = (Test-PythonModule $python "whisper")
	if ($kokoroOk -and $soundfileOk) {
		Write-Ok "Kokoro Python packages detected"
	} else {
		Write-Warn "Kokoro Python packages missing (`kokoro` and/or `soundfile`)"
		$issues.Add("Kokoro Python packages missing")
        if (Confirm-Install "Kokoro Python packages") {
            & $python -m pip install --upgrade pip
            & $python -m pip install "kokoro>=0.9.4" soundfile
			$actions.Add("Kokoro Python packages installed")
		}
	}
	if ($whisperOk -or (Test-Command "whisper")) {
		Write-Ok "Whisper STT detected"
	} else {
		Write-Warn "Whisper STT missing - Telegram voice notes cannot be transcribed"
		$issues.Add("Whisper STT missing for Telegram voice-in")
		if (Confirm-Install "Whisper STT Python package") {
			& $python -m pip install --upgrade pip
			& $python -m pip install openai-whisper
			$actions.Add("Whisper STT Python package installed")
		}
	}
}

if (Test-Command "espeak-ng") {
    Write-Ok "espeak-ng detected"
} else {
    Write-Warn "espeak-ng not found - Kokoro English fallback/non-English phonemization may fail"
    Write-Warn "  Install from https://github.com/espeak-ng/espeak-ng/releases if Kokoro reports espeak errors."
    $issues.Add("espeak-ng optional dependency missing")
}

Write-Head "Frontend dependencies"
if (-not $Check -and (Test-Command "npm")) {
    Push-Location (Join-Path $PSScriptRoot "frontend")
    try {
        if ($Auto -or (Confirm-Install "frontend npm packages")) {
            npm install
            $actions.Add("frontend npm install completed")
        }
    } finally {
        Pop-Location
    }
}

Write-Host ""
Write-Host "------------------------------------------------------------" -ForegroundColor DarkGray
Write-Host " Setup Summary" -ForegroundColor White
Write-Host "------------------------------------------------------------" -ForegroundColor DarkGray
if ($actions.Count -gt 0) {
    Write-Host "`n  Completed:" -ForegroundColor Green
    foreach ($a in $actions) { Write-Host "    + $a" -ForegroundColor Green }
}
if ($issues.Count -eq 0) {
    Write-Host "`n  Everything looks good." -ForegroundColor Green
} else {
    Write-Host "`n  Notes / action required:" -ForegroundColor Yellow
    foreach ($i in $issues) { Write-Host "    ! $i" -ForegroundColor Yellow }
}
Write-Host ""
