# ETH Sequential Wallet Scanner

Generate dan cek saldo wallet Ethereum secara berurutan mulai dari private key `0x000...001`.

## Cara Compile

```bash
# Pastikan Go sudah terinstall
go build -o eth-scanner .
```

Atau gunakan Makefile:
```bash
make build
```

## Cara Pakai

### 1. Generate Only (tanpa cek saldo)

```bash
./eth-scanner -gen -start 1 -end 100
```

Output:
```
No.     Private Key                                                       Address
─────────────────────────────────────────────────────────────────────────
1       0000000000000000000000000000000000000000000000000000000000000001  0x7E5F4552...
2       0000000000000000000000000000000000000000000000000000000000000002  0x2B5AD5c4...
```

### 2. Generate + Cek Saldo (scan mode)

```bash
./eth-scanner -start 1 -end 10000 -workers 20 -rpc https://eth.llamarpc.com
```

Wallet yang punya saldo disimpan otomatis ke `found_wallets.txt`.

### 3. Tampilkan Semua Wallet (termasuk saldo 0)

```bash
./eth-scanner -start 1 -end 100 -all
```

## Semua Flag

| Flag | Default | Keterangan |
|------|---------|-----------|
| `-start` | `1` | Index awal (desimal atau hex `0x...`) |
| `-end` | `1000` | Index akhir |
| `-workers` | `CPU*2` | Jumlah goroutine paralel |
| `-rpc` | llamarpc | Ethereum RPC endpoint |
| `-rate` | `100` | Jeda antar request per worker (ms) |
| `-timeout` | `10` | HTTP timeout per request (detik) |
| `-all` | `false` | Tampilkan semua wallet |
| `-output` | `found_wallets.txt` | File output wallet dengan saldo |
| `-gen` | `false` | Hanya generate address, tanpa cek saldo |

## Optimasi Performa

Script ini menggunakan beberapa teknik optimasi:

1. **Worker Pool (Goroutines)** — Multiple goroutine paralel untuk cek saldo secara bersamaan
2. **Connection Pool** — HTTP client dengan persistent connection pool (`MaxIdleConns`)
3. **Buffered Channels** — Channel buffered untuk mengurangi blocking antar goroutine
4. **Buffered Writer** — File output menggunakan `bufio.Writer` dengan buffer 64KB
5. **Rate Limiting per Worker** — Setiap worker memiliki rate limiter sendiri, bukan global
6. **`big.Int` reuse** — Operasi aritmatika meminimalkan alokasi memori baru
7. **Context-aware cancellation** — Bisa dihentikan kapan saja dengan `Ctrl+C`

## RPC Publik (Gratis)

- `https://eth.llamarpc.com` (default)
- `https://rpc.ankr.com/eth`
- `https://cloudflare-eth.com`
- `https://1rpc.io/eth`

Untuk performa terbaik, gunakan RPC berbayar seperti Alchemy atau Infura.

## Contoh Output file `found_wallets.txt`

```
# ETH Wallet Scanner — Found Wallets
# Format: INDEX | PRIVATE_KEY | ADDRESS | BALANCE_ETH

123 | 000...007b | 0xABC...123 | 0.50000000 ETH
```
