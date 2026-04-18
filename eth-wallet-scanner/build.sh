#!/usr/bin/env bash
set -e

if command -v go &>/dev/null; then
    GO=go
else
    GO=$(ls /nix/store/*/bin/go 2>/dev/null | grep "go-1" | head -1)
    if [ -z "$GO" ]; then
        echo "ERROR: Go tidak ditemukan."
        exit 1
    fi
fi

echo "Menggunakan: $($GO version)"
echo "Building..."

$GO build -ldflags="-s -w" -o eth-scan .

echo ""
echo "✓ Build selesai! Binary: ./eth-scan"
echo ""
echo "Contoh:"
echo "  ./eth-scan -gen -start 1 -end 10   # generate 10 wallet"
echo "  ./eth-scan -start 1 -end 1000      # scan saldo"
echo "  bash run.sh                         # jalankan otomatis (pakai konfigurasi run.sh)"
