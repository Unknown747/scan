#!/usr/bin/env bash
# =============================================
#   ETH Wallet Scanner — Konfigurasi Runner
#   Edit nilai di bawah sesuai kebutuhan
# =============================================

START=1                          # Index awal
END=99999999                     # Index akhir
WORKERS=5                        # Goroutine paralel (5 cukup untuk RPC publik)
RATE=300                         # Jeda per worker (ms) — naikkan jika banyak error
RPC="https://eth.llamarpc.com"   # RPC endpoint (ganti ke Alchemy/Infura untuk lebih cepat)
OUTPUT="found_wallets.txt"       # File simpan hasil yang ada saldo
TIMEOUT=15                       # HTTP timeout (detik)

# =============================================
# Jalankan scanner (jangan ubah bagian ini)
# =============================================
DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/eth-scan"

# Build otomatis jika binary belum ada
if [ ! -f "$BIN" ]; then
    echo "[*] Binary belum ada, building..."
    GO=$(ls /nix/store/*/bin/go 2>/dev/null | grep "go-1" | head -1)
    if [ -z "$GO" ]; then
        echo "ERROR: Go tidak ditemukan."
        exit 1
    fi
    cd "$DIR" && "$GO" build -ldflags="-s -w" -o eth-scan .
    echo "[✓] Build selesai."
fi

exec "$BIN" \
    -start "$START" \
    -end "$END" \
    -workers "$WORKERS" \
    -rate "$RATE" \
    -rpc "$RPC" \
    -output "$OUTPUT" \
    -timeout "$TIMEOUT"
