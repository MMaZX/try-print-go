#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$SCRIPT_DIR/usqay-print-client"

BUILD_LINUX_DIR="$CLIENT_DIR/build/linux"
BUILD_WINDOWS_DIR="$CLIENT_DIR/build/windows"

mkdir -p "$BUILD_LINUX_DIR"
mkdir -p "$BUILD_WINDOWS_DIR"

cd "$CLIENT_DIR"

echo "==> Compilando usqay-print-client para Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -o "$BUILD_LINUX_DIR/usqay-print-client" ./cmd/client
echo "    OK: $BUILD_LINUX_DIR/usqay-print-client"

echo "==> Compilando usqay-print-client para Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -o "$BUILD_WINDOWS_DIR/usqay-print-client.exe" ./cmd/client
echo "    OK: $BUILD_WINDOWS_DIR/usqay-print-client.exe"

echo "==> Copiando archivos de configuración..."
cp "$CLIENT_DIR/config.json" "$BUILD_LINUX_DIR/config.json"
cp "$CLIENT_DIR/config.json" "$BUILD_WINDOWS_DIR/config.json"
echo "    OK: Configuración copiada a ambos directorios."

echo "==> Iniciando usqay-print-client (Linux) en primer plano..."
cd "$BUILD_LINUX_DIR"
./usqay-print-client
