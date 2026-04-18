# ETH Sequential Wallet Scanner

## Overview

Tool Go untuk generate dan cek saldo wallet Ethereum secara berurutan, mulai dari private key `0x000...001`.

## Stack

- **Bahasa**: Go 1.21
- **Library**: `github.com/ethereum/go-ethereum/crypto`

## Struktur File

```
eth-wallet-scanner/
├── main.go              — CLI utama, flag, mode generate/scan
├── wallet/wallet.go     — Generate wallet dari index sequential
├── checker/checker.go   — Worker pool paralel, RPC balance check, retry
├── run.sh               — Konfigurasi + auto-run (edit di sini untuk ubah setting)
├── eth-scan             — Binary yang sudah dikompilasi
├── go.mod
└── go.sum
```

## Cara Jalankan

```bash
# Jalankan langsung (pakai konfigurasi dari run.sh)
bash eth-wallet-scanner/run.sh

# Generate wallet saja (tanpa cek saldo)
./eth-wallet-scanner/eth-scan -gen -start 1 -end 100

# Scan saldo
./eth-wallet-scanner/eth-scan -start 1 -end 10000 -workers 5
```

## Ubah Konfigurasi

Edit bagian atas file `eth-wallet-scanner/run.sh`:

```bash
START=1                        # Index awal
END=99999999                   # Index akhir
WORKERS=5                      # Goroutine paralel
RATE=300                       # Jeda per worker (ms)
RPC="https://eth.llamarpc.com" # RPC endpoint
```

## Compile Ulang

```bash
cd eth-wallet-scanner
/nix/store/a90l6nxkqdlqxzgz5j958rz5gwygbamc-go-1.21.13/bin/go build -ldflags="-s -w" -o eth-scan .
```

## Output

Wallet yang punya saldo disimpan otomatis ke `found_wallets.txt`.
