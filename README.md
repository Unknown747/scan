# ETH Sequential Wallet Scanner

Scanner dompet Ethereum berkinerja tinggi yang men-generate private key secara sekuensial dan memeriksa saldo di jaringan Ethereum melalui JSON-RPC.

---

## Prasyarat

- **Go** versi 1.21 atau lebih baru
- Koneksi internet (untuk mengakses RPC Ethereum)

Cek apakah Go sudah terinstall:

```bash
go version
```

---

## Instalasi

### 1. Clone repository

```bash
git clone <URL_REPO>
cd <NAMA_FOLDER>
```

### 2. Download dependensi

```bash
cd eth-wallet-scanner
go mod tidy
cd ..
```

### 3. Build binary

```bash
cd eth-wallet-scanner
go build -ldflags="-s -w" -o ../eth-scan .
cd ..
```

Binary `eth-scan` akan tersedia di folder root.

---

## Konfigurasi

Semua konfigurasi ada di file **`run.sh`**. Edit sesuai kebutuhan:

```bash
START=1                        # Index awal scan
END=99999999                   # Index akhir scan
WORKERS=5                      # Jumlah goroutine paralel
BATCH=20                       # Jumlah wallet per request RPC
RATE=300                       # Jeda antar batch (milidetik)
TIMEOUT=15                     # Timeout HTTP (detik)
OUTPUT="found_wallets.txt"     # File output wallet yang punya saldo
LAST="last_key.txt"            # File progress (resume otomatis)
FOUND_ONLY=false               # true = hanya tampilkan wallet dengan saldo

RPC="https://eth.llamarpc.com" # RPC endpoint (bisa lebih dari satu, pisahkan koma)
```

### Menggunakan beberapa RPC (load balancing)

```bash
RPC="https://rpc1.example.com,https://rpc2.example.com"
```

---

## Notifikasi Telegram (Opsional)

Edit file **`telegram.json`** dengan token bot dan chat ID kamu:

```json
{
  "token": "TOKEN_BOT_KAMU",
  "chat_id": "CHAT_ID_KAMU"
}
```

Jika token tidak diisi atau masih berisi placeholder `YOUR_BOT_TOKEN`, notifikasi Telegram dinonaktifkan secara otomatis.

---

## Menjalankan Scanner

```bash
bash run.sh
```

Atau jalankan binary langsung dengan flag kustom:

```bash
./eth-scan \
  -start 1 \
  -end 1000000 \
  -workers 10 \
  -batch 20 \
  -rate 300 \
  -rpc "https://eth.llamarpc.com" \
  -output found_wallets.txt \
  -last last_key.txt
```

### Mode generate-only (tanpa cek saldo)

```bash
./eth-scan -gen -start 1 -end 100
```

### Hanya tampilkan wallet yang punya saldo

```bash
./eth-scan -found-only -start 1 -end 99999999
```

---

## Resume Otomatis

Scanner menyimpan progress ke **`last_key.txt`** secara otomatis setiap kali sebuah batch selesai diproses. Jika scanner dihentikan (Ctrl+C atau restart), scan akan dilanjutkan dari index terakhir yang **sudah selesai diproses** — tidak ada wallet yang terlewati.

> **Catatan:** Untuk mulai scan ulang dari awal, hapus file `last_key.txt` terlebih dahulu:
> ```bash
> rm last_key.txt
> ```

---

## Output

Wallet yang memiliki saldo akan disimpan ke `found_wallets.txt` dengan format:

```
COUNT | PRIVATE_KEY | ADDRESS | BALANCE_ETH
```

Contoh:
```
42 | 000...002a | 0xAbC...123 | 0.50000000 ETH
```

---

## Struktur Folder

```
.
├── eth-wallet-scanner/
│   ├── main.go          # Entry point, orkestrasi worker
│   ├── checker/
│   │   └── checker.go   # Logic scan & batch RPC
│   ├── wallet/
│   │   └── wallet.go    # Generate wallet dari index
│   ├── go.mod
│   └── go.sum
├── run.sh               # Script konfigurasi & jalankan scanner
├── telegram.json        # Konfigurasi notifikasi Telegram
├── eth-scan             # Binary hasil build
├── found_wallets.txt    # Output wallet dengan saldo (auto-generated)
└── last_key.txt         # File progress resume (auto-generated)
```

---

## Flag Lengkap

| Flag | Default | Keterangan |
|------|---------|------------|
| `-start` | `1` | Index private key awal |
| `-end` | `1000` | Index private key akhir |
| `-workers` | `CPU*2` | Jumlah goroutine paralel |
| `-batch` | `20` | Jumlah wallet per request RPC |
| `-rate` | `300` | Jeda antar batch per worker (ms) |
| `-timeout` | `15` | Timeout HTTP (detik) |
| `-rpc` | llamarpc.com | RPC endpoint, pisahkan koma untuk multiple |
| `-output` | `found_wallets.txt` | File output wallet dengan saldo |
| `-last` | `last_key.txt` | File untuk menyimpan & resume progress |
| `-gen` | `false` | Generate alamat saja tanpa cek saldo |
| `-found-only` | `false` | Hanya tampilkan wallet yang punya saldo |
| `-tg` | `../telegram.json` | Path ke file konfigurasi Telegram |
