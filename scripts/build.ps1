param (
    [string]$Target = "build-release"
)

$BINARY_NAME = "opendeploy"
$LDFLAGS = "-s -w"

# Ensure we execute from the backend directory regardless of where the script is called from
$ScriptPath = Split-Path -Parent $MyInvocation.MyCommand.Definition
$BackendPath = Join-Path $ScriptPath "..\backend"
Set-Location $BackendPath

# Helper function to build for a specific architecture
function Build-Binary {
    param (
        [string]$goos,
        [string]$goarch,
        [string]$goarm,
        [string]$suffix
    )
    Write-Host "Building for $goos/$goarch..."
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    if ($goarm) { $env:GOARM = $goarm } else { Remove-Item Env:\GOARM -ErrorAction SilentlyContinue }
    
    $outputName = if ($suffix) { "$BINARY_NAME-$suffix" } else { $BINARY_NAME }
    if ($goos -eq "windows") { $outputName += ".exe" }
    
    go build -ldflags="$LDFLAGS" -o $outputName ./cmd/opendeploy
    
    Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARM -ErrorAction SilentlyContinue
}

function Build-Frontend {
    Write-Host "Building frontend..."
    Set-Location ../frontend
    npm install
    npm run build
    Set-Location ../backend
}

switch ($Target) {
    "build-frontend" {
        Build-Frontend
    }
    "build-linux-arm64" {
        Build-Binary "linux" "arm64" "" "linux-arm64"
    }
    "build-linux-amd64" {
        Build-Binary "linux" "amd64" "" "linux-amd64"
    }
    "build-linux-arm" {
        Build-Binary "linux" "arm" "7" "linux-armv7"
    }
    "build-linux-386" {
        Build-Binary "linux" "386" "" "linux-386"
    }
    "build-all" {
        Build-Frontend
        Build-Binary "linux" "arm64" "" "linux-arm64"
        Build-Binary "linux" "amd64" "" "linux-amd64"
        Build-Binary "linux" "arm" "7" "linux-armv7"
        Build-Binary "linux" "386" "" "linux-386"
    }
    "build-release" {
        Build-Frontend
        Build-Binary "linux" "arm64" "" "linux-arm64"
        Write-Host "Release binary: $BINARY_NAME-linux-arm64"
    }
    "build" {
        $env:GOOS = ""
        $env:GOARCH = ""
        go build -ldflags="$LDFLAGS" -o $BINARY_NAME ./cmd/opendeploy
    }
    default {
        Write-Host "Unknown target: $Target"
        Write-Host "Valid targets: build, build-frontend, build-linux-arm64, build-linux-amd64, build-linux-arm, build-linux-386, build-all, build-release"
    }
}
