# PowerShell script to build MQTT server as Windows executable

Write-Host "Building MQTT Server as Windows executable..." -ForegroundColor Green

# Set environment variables for Windows build
$env:GOOS = "windows"
$env:GOARCH = "amd64"

# Create bin directory if it doesn't exist
if (!(Test-Path "bin")) {
    New-Item -ItemType Directory -Path "bin"
    Write-Host "Created bin directory" -ForegroundColor Yellow
}

# Build the server
Write-Host "Compiling server..." -ForegroundColor Blue
go build -o bin/server.exe ./cmd/server

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Server executable built successfully: bin/server.exe" -ForegroundColor Green
    
    # Get file size
    $fileSize = (Get-Item "bin/server.exe").Length
    $fileSizeMB = [math]::Round($fileSize / 1MB, 2)
    Write-Host "File size: $fileSizeMB MB" -ForegroundColor Cyan
    
    Write-Host ""
    Write-Host "To run the server:" -ForegroundColor Yellow
    Write-Host "  .\bin\server.exe" -ForegroundColor White
} else {
    Write-Host "✗ Build failed!" -ForegroundColor Red
    exit 1
}

# Reset environment variables
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
