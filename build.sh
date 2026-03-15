#!/bin/bash

# ==========================================
# DraARL Unix Release Build Script
# Frontend + Backend, multi-platform
# ==========================================

set -e

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
echo ""

# Clean old build artifacts
echo "[1/4] Cleaning old build artifacts..."
rm -f "$BINARY_NAME" 2>/dev/null || true
rm -rf www/dist 2>/dev/null || true
rm -rf internal/server/web 2>/dev/null || true

# Build frontend
echo "[2/4] Building frontend..."
cd www
npm run build
cd ..

echo ""
echo "[3/4] Copying frontend dist to internal/server/web/dist..."
mkdir -p internal/server/web
cp -r www/dist internal/server/web/dist

echo ""
echo "[4/4] Building backend with embedded frontend..."
export CGO_ENABLED=0
go build -ldflags="-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME -X main.isRelease=true" -tags=embed -o "$BINARY_NAME" ./cmd/udphub

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

# Clean intermediate files (keep www/dist for development)
echo ""
echo "Cleaning intermediate files..."
rm -rf internal/server/web 2>/dev/null || true

echo ""
echo "Done! Binary: $BINARY_NAME"
