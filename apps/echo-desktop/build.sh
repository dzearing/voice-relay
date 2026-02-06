#!/bin/bash
# Build script for Voice Relay desktop app.
# Builds the PWA first, then copies dist into the Go embed directory, then builds Go.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PWA_DIR="$REPO_ROOT/packages/pwa"
PWA_DIST="$SCRIPT_DIR/internal/coordinator/pwa_dist"

echo "==> Building PWA..."
cd "$PWA_DIR"
npm install
npm run build

echo "==> Copying PWA dist to Go embed directory..."
rm -rf "$PWA_DIST"/*
cp -r "$PWA_DIR/dist/"* "$PWA_DIST/"

echo "==> Building Go binary..."
cd "$SCRIPT_DIR"
go mod tidy
CGO_ENABLED=1 go build -o VoiceRelay -ldflags "-H windowsgui" .

echo "==> Done! Binary: $SCRIPT_DIR/VoiceRelay"
