#!/usr/bin/env bash
# =============================================
#   ETH Wallet Scanner — Configuration
#   Edit the values below as needed
# =============================================

START=1                          # Starting index (ignored if last_key.txt exists)
END=99999999                     # Ending index
WORKERS=5                        # Parallel goroutines
BATCH=20                         # Wallets per RPC batch request
RATE=300                         # Delay between batches per worker (ms)
RPC="https://eth.llamarpc.com"   # RPC endpoint
OUTPUT="found_wallets.txt"       # Output file for wallets with balance
LAST="last_key.txt"              # File to save & resume last index
TIMEOUT=15                       # HTTP timeout (seconds)

# =============================================
ROOT="$(cd "$(dirname "$0")" && pwd)"
BIN="$ROOT/eth-scan"
SRC="$ROOT/eth-wallet-scanner"

if [ ! -f "$BIN" ]; then
    echo "[*] Building binary..."
    if ! command -v go &>/dev/null; then
        echo "ERROR: Go not found."
        exit 1
    fi
    cd "$SRC" && GOFLAGS="-mod=mod" go build -ldflags="-s -w" -o "$BIN" . || exit 1
    echo "[✓] Build complete."
fi

cd "$ROOT"

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
