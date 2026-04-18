#!/usr/bin/env bash
# =============================================
#   ETH Wallet Scanner — Konfigurasi Runner
#   Edit nilai di bawah sesuai kebutuhan
# =============================================

START=1                          # Index awal (diabaikan jika last_key.txt ada)
END=99999999                     # Index akhir
WORKERS=5                        # Goroutine paralel
BATCH=20                         # Wallet per batch RPC (1 request = 20 wallet)
RATE=300                         # Jeda antar batch per worker (ms)
RPC="https://eth.llamarpc.com"   # RPC endpoint (Alchemy/Infura lebih cepat)
OUTPUT="found_wallets.txt"       # File simpan hasil yang ada saldo
LAST="last_key.txt"              # File simpan & resume index terakhir
TIMEOUT=15                       # HTTP timeout (detik)

# =============================================
# Jalankan scanner (jangan ubah bagian ini)
# =============================================
DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/eth-scan"

if [ ! -f "$BIN" ]; then
    echo "[*] Binary belum ada, building..."
    if ! command -v go &>/dev/null; then
        echo "ERROR: Go tidak ditemukan."
        exit 1
    fi
    cd "$DIR" && go build -ldflags="-s -w" -o eth-scan .
    echo "[✓] Build selesai."
fi

exec "$BIN" \
    -start "$START" \
    -end "$END" \
    -workers "$WORKERS" \
    -batch "$BATCH" \
    -rate "$RATE" \
    -rpc "$RPC" \
    -output "$OUTPUT" \
    -last "$LAST" \
    -timeout "$TIMEOUT"
