param(
    [switch]$Run,
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    throw "wails CLI was not found on PATH. Install Wails or open a shell where wails is available."
}

if (-not $SkipTests) {
    go test ./...
    go vet ./...
}

Push-Location "$Root\frontend"
try {
    npm run build
}
finally {
    Pop-Location
}

$plainGoBinary = Join-Path $Root "mauler.exe"
if (Test-Path $plainGoBinary) {
    Remove-Item -LiteralPath $plainGoBinary -Force
}

Get-Process -Name TheMauler -ErrorAction SilentlyContinue | Stop-Process -Force
wails build -clean

$exe = Join-Path $Root "build\bin\TheMauler.exe"
Write-Host "Built $exe"

if ($Run) {
    Start-Process -FilePath $exe
}
