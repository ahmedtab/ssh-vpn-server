#!/usr/bin/env bash
# Build script for LinkThings Client - Linux amd64
# Usage: ./build.sh [version] (default: 1.0.0.0)
set -euo pipefail

if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is not installed or not in PATH" >&2
    exit 1
fi

VERSION="${1:-1.0.0.0}"
OUTPUT_DIR="./dist"
mkdir -p "$OUTPUT_DIR"

echo "Building LinkThings Client v${VERSION} for Linux amd64..."

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -o "${OUTPUT_DIR}/linkthings-client.linux.amd64" \
    -ldflags "-X main.Version=${VERSION}" \
    .

echo "Build completed successfully!"
echo "Output: ${OUTPUT_DIR}/linkthings-client.linux.amd64"
