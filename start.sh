#!/bin/sh
set -e

# Jalankan PO Token provider di background, listen di localhost:4416 (default)
node /opt/bgutil-provider/server/build/main.js &

# Tunggu sebentar supaya provider server siap sebelum bot mulai
sleep 5

# Jalankan bot Go utama (foreground, PID 1)
exec ./bot-afk
