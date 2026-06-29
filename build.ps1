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

Get-Process -Name TheMauler -ErrorAction SilentlyContinue | Stop-Process -Force
Invoke-Checked wails build -clean

$exe = Join-Path $Root "build\bin\TheMauler.exe"
Write-Host "Built $exe"

if ($Run) {
    Start-Process -FilePath $exe
}
