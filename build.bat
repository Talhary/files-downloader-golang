@echo off
echo =======================================================================
echo Building Go Multi-Part Download Engine for Windows and Linux Ubuntu...
echo =======================================================================

if not exist bin mkdir bin

echo [1/3] Compiling Linux amd64 (Ubuntu / GitHub Actions)...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o bin/dlengine-linux-amd64 ./cmd/dlengine

echo [2/3] Compiling Linux arm64 (AWS Graviton / Apple Silicon Docker)...
set GOOS=linux
set GOARCH=arm64
go build -trimpath -ldflags="-s -w" -o bin/dlengine-linux-arm64 ./cmd/dlengine

echo [3/3] Compiling Windows amd64...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w" -o bin\dlengine-windows-amd64.exe ./cmd/dlengine

echo =======================================================================
echo Build complete! Binaries located in ./bin
echo =======================================================================
dir bin
