#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$SCRIPT_DIR/usqay-print-client"

BUILD_LINUX_DIR="$CLIENT_DIR/build/linux"
BUILD_WINDOWS_DIR="$CLIENT_DIR/build/windows"

TARGET=""
for arg in "$@"; do
  case "$arg" in
    --target=*) TARGET="${arg#--target=}" ;;
  esac
done

if [[ -z "$TARGET" ]]; then
  echo "Error: debes especificar --target=host o --target=docker"
  exit 1
fi

if [[ "$TARGET" != "host" && "$TARGET" != "docker" ]]; then
  echo "Error: --target debe ser 'host' o 'docker'"
  exit 1
fi

mkdir -p "$BUILD_LINUX_DIR"
mkdir -p "$BUILD_WINDOWS_DIR"

if [[ "$TARGET" == "host" ]]; then
  cd "$CLIENT_DIR"

  echo "==> Compilando usqay-print-client para Linux (amd64)..."
  GOOS=linux GOARCH=amd64 go build -o "$BUILD_LINUX_DIR/usqay-print-client" ./cmd/client
  echo "    OK: $BUILD_LINUX_DIR/usqay-print-client"

  echo "==> Compilando usqay-print-client para Windows (amd64)..."
  GOOS=windows GOARCH=amd64 go build -o "$BUILD_WINDOWS_DIR/usqay-print-client.exe" ./cmd/client
  echo "    OK: $BUILD_WINDOWS_DIR/usqay-print-client.exe"

elif [[ "$TARGET" == "docker" ]]; then
  cd "$SCRIPT_DIR"

  echo "==> Compilando usqay-print-client para Linux (amd64) via Docker..."
  docker compose run --rm client-linux-build
  echo "    OK: $BUILD_LINUX_DIR/usqay-print-client"

  echo "==> Compilando usqay-print-client para Windows (amd64) via Docker..."
  docker compose run --rm client-windows-build
  echo "    OK: $BUILD_WINDOWS_DIR/usqay-print-client.exe"
fi

echo "==> Copiando archivos de configuración..."
cp "$CLIENT_DIR/config.json" "$BUILD_LINUX_DIR/config.json"
cp "$CLIENT_DIR/config.json" "$BUILD_WINDOWS_DIR/config.json"
echo "    OK: Configuración copiada a ambos directorios."

echo "==> Iniciando usqay-print-client (Linux) en primer plano..."
cd "$BUILD_LINUX_DIR"
./usqay-print-client
