# Script to download Whisper models for OhMyVoice
Param(
    [string]$Model = "small" # base, small, medium, all
)

$ErrorActionPreference = "Stop"
$modelsDir = Join-Path $PSScriptRoot "models"
if (!(Test-Path $modelsDir)) {
    New-Item -ItemType Directory -Path $modelsDir | Out-Null
}

$baseUrl = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

$modelFiles = @{
    "base"   = "ggml-base.bin"
    "small"  = "ggml-small.bin"
    "medium" = "ggml-medium.bin"
}

function Download-Model($name, $file) {
    $targetPath = Join-Path $modelsDir $file
    if (Test-Path $targetPath) {
        Write-Host " [OK] Model '$name' ($file) already exists." -ForegroundColor Green
        return
    }
    $url = "$baseUrl/$file"
    Write-Host " [DOWNLOAD] Fetching $name model from $url..." -ForegroundColor Cyan
    curl.exe -L -o $targetPath $url
    Write-Host " [OK] Model '$name' successfully downloaded!" -ForegroundColor Green
}

if ($Model -eq "all") {
    Download-Model "base" $modelFiles["base"]
    Download-Model "small" $modelFiles["small"]
    Download-Model "medium" $modelFiles["medium"]
} elseif ($modelFiles.ContainsKey($Model.ToLower())) {
    Download-Model $Model.ToLower() $modelFiles[$Model.ToLower()]
} else {
    Write-Host "Unknown model: $Model. Available: base, small, medium, all" -ForegroundColor Red
}
