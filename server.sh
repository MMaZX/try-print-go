#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$SCRIPT_DIR/usqay-print-server"
BUILD_DIR="$SERVER_DIR/build"

TARGET=""
BUILD_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --target=*) TARGET="${arg#--target=}" ;;
    --build) BUILD_ONLY=1 ;;
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

echo "==> Build usqay-print-server (target: $TARGET)..."

if [[ "$TARGET" == "host" ]]; then
  cd "$SERVER_DIR"
  go build -o "$BUILD_DIR/usqay-print-server" ./cmd/server
elif [[ "$TARGET" == "docker" ]]; then
  cd "$SCRIPT_DIR"
  docker compose run --rm server-build
fi

echo "    OK: $BUILD_DIR/usqay-print-server"

echo "==> Copiando config.json..."
cp "$SERVER_DIR/config.json" "$BUILD_DIR/config.json"
echo "    OK: $BUILD_DIR/config.json"

if [[ "$BUILD_ONLY" -eq 1 ]]; then
  echo "==> --build: binario listo, no se inicia."
  exit 0
fi

echo "==> Iniciando usqay-print-server en primer plano..."
cd "$BUILD_DIR"
./usqay-print-server
