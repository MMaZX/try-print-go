#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/usqay-print-server/build"
BINARY="$BUILD_DIR/usqay-print-server"
PM2_NAME="usqay-print-server-try"

if ! command -v pm2 &>/dev/null; then
  echo "Error: pm2 no está instalado. Instálalo con: npm install -g pm2"
  exit 1
fi

if [[ ! -f "$BINARY" ]]; then
  echo "Error: binario no encontrado en $BINARY"
  echo "       Ejecuta primero: ./server.sh --target=host  o  ./server.sh --target=docker"
  exit 1
fi

if [[ ! -f "$BUILD_DIR/config.json" ]]; then
  echo "Error: config.json no encontrado en $BUILD_DIR"
  echo "       Copia o crea config.json junto al binario antes de continuar."
  exit 1
fi

chmod +x "$BINARY"

echo "==> Registrando '$PM2_NAME' en PM2..."

if pm2 describe "$PM2_NAME" &>/dev/null; then
  pm2 restart "$PM2_NAME"
  echo "    OK: proceso reiniciado."
else
  pm2 start "$BINARY" \
    --name "$PM2_NAME" \
    --cwd "$BUILD_DIR"
  echo "    OK: proceso iniciado."
fi

pm2 save
echo "==> Estado actual:"
pm2 show "$PM2_NAME"
