#!/bin/bash

# ==========================================
# DraARL Unix Release Build Script
# ==========================================

BINARY_NAME="DraARL"

# Get version from git
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Get build time
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "=========================================="
echo "DraARL Release Build"
echo "=========================================="
echo "Version:    $VERSION"
echo "Build Time: $BUILD_TIME"
echo "Binary:     $BINARY_NAME"
echo "=========================================="

# Clean old binary
if [ -f "$BINARY_NAME" ]; then
    echo "Cleaning old binary..."
    rm -f "$BINARY_NAME"
fi

# Build
echo "Building..."
go build -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME -X main.isRelease=true" -o "$BINARY_NAME" ./cmd/udphub

if [ $? -eq 0 ]; then
    echo ""
    echo "=========================================="
    echo "Build successful!"
    echo "=========================================="
    echo "Size: $(stat -f%z "$BINARY_NAME" 2>/dev/null || stat -c%s "$BINARY_NAME" 2>/dev/null) bytes"
    echo ""
    echo "Version info:"
    ./"$BINARY_NAME" -v
else
    echo ""
    echo "=========================================="
    echo "Build FAILED!"
    echo "=========================================="
    exit 1
fi
