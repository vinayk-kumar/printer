# Print Agent installer for Windows (PowerShell):
#   irm https://raw.githubusercontent.com/vinayk-kumar/printer/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'vinayk-kumar/printer'
$sumatraVersion = '3.5.2'
$dir = Join-Path $env:LOCALAPPDATA 'PrintAgent'
New-Item -ItemType Directory -Force -Path $dir | Out-Null

# Stop a running agent so its files can be replaced.
Get-Process print-agent -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host 'Downloading Print Agent...'
$setup = Join-Path $env:TEMP 'print-agent-setup.exe'
Invoke-WebRequest -UseBasicParsing "https://github.com/$repo/releases/latest/download/print-agent-windows-amd64.exe" -OutFile $setup

# SumatraPDF prints PDFs silently; Windows has no built-in way to do that.
$sumatra = Join-Path $dir 'SumatraPDF.exe'
if (-not (Test-Path $sumatra)) {
    Write-Host 'Downloading SumatraPDF (used to print PDFs)...'
    $zip = Join-Path $env:TEMP 'sumatrapdf.zip'
    Invoke-WebRequest -UseBasicParsing "https://www.sumatrapdfreader.org/dl/rel/$sumatraVersion/SumatraPDF-$sumatraVersion-64.zip" -OutFile $zip
    Expand-Archive -Path $zip -DestinationPath $dir -Force
    Move-Item -Force (Join-Path $dir "SumatraPDF-$sumatraVersion-64.exe") $sumatra
    Remove-Item $zip
}

& $setup install
Remove-Item $setup -ErrorAction SilentlyContinue
