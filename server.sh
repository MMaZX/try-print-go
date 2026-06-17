#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$SCRIPT_DIR/usqay-print-server"
BUILD_DIR="$SERVER_DIR/build"
SESSION="print-server"

echo "==> Build usqay-print-server..."
cd "$SERVER_DIR"
go build -o "$BUILD_DIR/usqay-print-server" ./cmd/server
echo "    OK: $BUILD_DIR/usqay-print-server"

echo "==> Copiando config.json..."
cp "$SERVER_DIR/config.json" "$BUILD_DIR/config.json"
echo "    OK: $BUILD_DIR/config.json"

echo "==> Levantando en tmux (sesión: $SESSION)..."
# Si ya existe la sesión, la mata y arranca de nuevo
tmux kill-session -t "$SESSION" 2>/dev/null || true
tmux new-session -d -s "$SESSION" -c "$BUILD_DIR" "$BUILD_DIR/usqay-print-server"

echo ""
echo "  Servidor corriendo en http://localhost:8090"
echo ""
echo "  Ver logs:       tmux attach -t $SESSION"
echo "  Desacoplar:     Ctrl+B, luego D"
echo "  Detener:        tmux kill-session -t $SESSION"
echo "  Health check:   curl http://localhost:8090/api/status"
