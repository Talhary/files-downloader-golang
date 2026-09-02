#!/usr/bin/env bash
set -e

echo "======================================================================="
echo "Building Go Multi-Part Download Engine for Windows and Linux Ubuntu..."
echo "======================================================================="

mkdir -p bin

echo "[1/3] Compiling Linux amd64 (Ubuntu / GitHub Actions)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/dlengine-linux-amd64 ./cmd/dlengine

echo "[2/3] Compiling Linux arm64 (AWS Graviton / ARM runners)..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/dlengine-linux-arm64 ./cmd/dlengine

echo "[3/3] Compiling Windows amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/dlengine-windows-amd64.exe ./cmd/dlengine

chmod +x bin/dlengine-linux-amd64 bin/dlengine-linux-arm64

echo "======================================================================="
echo "Build complete! Binaries located in ./bin/"
echo "======================================================================="
ls -lh bin/
