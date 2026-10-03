#!/usr/bin/env bash

set -e

BINARY_NAME="opendeploy"
LDFLAGS="-s -w"

# Move to the root of the project
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

build_frontend() {
    echo "Installing frontend dependencies..."
    cd "$ROOT_DIR/frontend"
    npm install
    echo "Building frontend..."
    npm run build
}

build_binary() {
    local os=$1
    local arch=$2
    local arm=$3
    local suffix=$4

    echo "Building for $os/$arch..."
    cd "$ROOT_DIR/backend"
    
    local output_name=$BINARY_NAME
    if [ -n "$suffix" ]; then
        output_name="$BINARY_NAME-$suffix"
    fi
    if [ "$os" = "windows" ]; then
        output_name="$output_name.exe"
    fi

    if [ -n "$arm" ]; then
        env GOOS=$os GOARCH=$arch GOARM=$arm go build -ldflags="$LDFLAGS" -o $output_name ./cmd/opendeploy
    else
        env GOOS=$os GOARCH=$arch go build -ldflags="$LDFLAGS" -o $output_name ./cmd/opendeploy
    fi
    
    echo "Built binary: $output_name"
}

case "${1:-build-release}" in
    build-frontend)
        build_frontend
        ;;
    build-linux-arm64)
        build_binary linux arm64 "" linux-arm64
        ;;
    build-linux-amd64)
        build_binary linux amd64 "" linux-amd64
        ;;
    build-linux-arm)
        build_binary linux arm 7 linux-armv7
        ;;
    build-linux-x86)
        build_binary linux 386 "" linux-x86
        ;;
    build-all)
        build_frontend
        build_binary linux arm64 "" linux-arm64
        build_binary linux amd64 "" linux-amd64
        build_binary linux arm 7 linux-armv7
        build_binary linux 386 "" linux-x86
        ;;
    build-release)
        build_frontend
        build_binary linux arm64 "" linux-arm64
        echo "Release binary: $BINARY_NAME-linux-arm64"
        ;;
    build)
        cd "$ROOT_DIR/backend"
        go build -ldflags="$LDFLAGS" -o $BINARY_NAME ./cmd/opendeploy
        ;;
    *)
        echo "Unknown target: $1"
        echo "Valid targets: build, build-frontend, build-linux-arm64, build-linux-amd64, build-linux-arm, build-linux-x86, build-all, build-release"
        exit 1
        ;;
esac
