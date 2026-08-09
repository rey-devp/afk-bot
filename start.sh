#!/bin/sh
set -e

echo "[START.SH] Starting PO Token provider..."
node /opt/bgutil-provider/server/build/main.js 2>&1 | sed 's/^/[POT-PROVIDER] /' &
POT_PID=$!

echo "[START.SH] Waiting for PO Token provider to be ready..."
sleep 3

# Cek apakah proses masih hidup setelah delay awal
if ! kill -0 $POT_PID 2>/dev/null; then
    echo "[START.SH] WARNING: PO Token provider process died during startup!"
fi

echo "[START.SH] Starting main bot..."
exec ./bot-afk
