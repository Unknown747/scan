#!/usr/bin/env bash
set -e

# Cari Go binary
if command -v go &>/dev/null; then
    GO=go
elif [ -f /nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go ]; then
    GO=/nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go
else
    # Cari di nix store
    GO=$(ls /nix/store/*/bin/go 2>/dev/null | head -1)
    if [ -z "$GO" ]; then
        echo "ERROR: Go tidak ditemukan. Install dulu: https://go.dev/dl/"
        exit 1
    fi
fi

echo "Menggunakan Go: $($GO version)"
echo "Building..."

$GO build -ldflags="-s -w" -o eth-scanner .

echo ""
echo "✓ Build berhasil! Binary: ./eth-scanner"
echo ""
echo "Contoh penggunaan:"
echo "  ./eth-scanner -gen -start 1 -end 10          # generate 10 wallet"
echo "  ./eth-scanner -start 1 -end 1000 -workers 20 # scan saldo 1000 wallet"
echo "  ./eth-scanner -help                           # lihat semua opsi"
