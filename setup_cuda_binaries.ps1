# Script to download and set up Whisper CUDA 12.4 binaries for OhMyVoice
$ErrorActionPreference = "Stop"
$binDir = Join-Path $PSScriptRoot "bin"
if (!(Test-Path $binDir)) {
    New-Item -ItemType Directory -Path $binDir | Out-Null
}

$cliPath = Join-Path $binDir "whisper-cli.exe"
if (Test-Path $cliPath) {
    Write-Host " [OK] whisper-cli.exe already exists in bin/." -ForegroundColor Green
    exit 0
}

$tempZip = Join-Path $binDir "whisper-cuda.zip"
$cudaUrl = "https://github.com/ggerganov/whisper.cpp/releases/download/b5130/whisper-cublas-12.4.0-bin-x64.zip"

Write-Host " [DOWNLOAD] Fetching Whisper CUDA 12.4 binaries..." -ForegroundColor Cyan
curl.exe -L -o $tempZip $cudaUrl

Write-Host " [EXTRACT] Unpacking to bin/..." -ForegroundColor Cyan
Expand-Archive -Path $tempZip -DestinationPath $binDir -Force
Remove-Item -Path $tempZip -Force

Write-Host " [OK] CUDA 12.4 binaries ready in bin/!" -ForegroundColor Green
