#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$SCRIPT_DIR/usqay-print-server"
BUILD_DIR="$SERVER_DIR/build"

echo "==> Build usqay-print-server..."
cd "$SERVER_DIR"
go build -o "$BUILD_DIR/usqay-print-server" ./cmd/server
echo "    OK: $BUILD_DIR/usqay-print-server"

echo "==> Copiando config.json..."
cp "$SERVER_DIR/config.json" "$BUILD_DIR/config.json"
echo "    OK: $BUILD_DIR/config.json"

echo "==> Iniciando usqay-print-server en primer plano..."
cd "$BUILD_DIR"
./usqay-print-server
