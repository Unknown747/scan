package main

import (
        "bufio"
        "context"
        "flag"
        "fmt"
        "math/big"
        "os"
        "os/signal"
        "runtime"
        "sync"
        "syscall"
        "time"

        "eth-wallet-scanner/checker"
        "eth-wallet-scanner/wallet"
)

const banner = `
╔═══════════════════════════════════════════════════╗
║         ETH Sequential Wallet Scanner             ║
║   Generate + Check Ethereum Wallets (0x001+)      ║
╚═══════════════════════════════════════════════════╝
`

func main() {
        // ===== CLI Flags =====
        var (
                startHex  = flag.String("start", "1", "Index awal (hex atau desimal), default: 1")
                endHex    = flag.String("end", "1000", "Index akhir (hex atau desimal), default: 1000")
                workers   = flag.Int("workers", runtime.NumCPU()*2, "Jumlah goroutine worker paralel")
                rpcURL    = flag.String("rpc", "https://eth.llamarpc.com", "Ethereum RPC endpoint")
                rateMs    = flag.Int("rate", 100, "Jeda antar request per worker (milidetik)")
                timeoutS  = flag.Int("timeout", 10, "HTTP timeout per request (detik)")
                showAll   = flag.Bool("all", false, "Tampilkan semua wallet (termasuk saldo 0)")
                outputFile = flag.String("output", "found_wallets.txt", "File output untuk wallet yang punya saldo")
                genOnly   = flag.Bool("gen", false, "Hanya generate address tanpa cek saldo")
        )
        flag.Parse()

        fmt.Print(banner)

        // Parse start index
        startIndex, ok := new(big.Int).SetString(*startHex, 0)
        if !ok || startIndex.Sign() <= 0 {
                // Coba parse sebagai desimal biasa
                startIndex, ok = new(big.Int).SetString(*startHex, 10)
                if !ok || startIndex.Sign() <= 0 {
                        fmt.Fprintf(os.Stderr, "ERROR: start tidak valid: %s\n", *startHex)
                        os.Exit(1)
                }
        }

        // Parse end index
        endIndex, ok := new(big.Int).SetString(*endHex, 0)
        if !ok {
                endIndex, ok = new(big.Int).SetString(*endHex, 10)
                if !ok {
                        fmt.Fprintf(os.Stderr, "ERROR: end tidak valid: %s\n", *endHex)
                        os.Exit(1)
                }
        }

        if startIndex.Cmp(endIndex) > 0 {
                fmt.Fprintf(os.Stderr, "ERROR: start harus <= end\n")
                os.Exit(1)
        }

        totalWallets := new(big.Int).Sub(endIndex, startIndex)
        totalWallets.Add(totalWallets, big.NewInt(1))

        fmt.Printf("  Start Index : 0x%064x\n", startIndex)
        fmt.Printf("  End Index   : 0x%064x\n", endIndex)
        fmt.Printf("  Total       : %s wallet\n", totalWallets.String())
        fmt.Printf("  Workers     : %d goroutines\n", *workers)
        fmt.Printf("  RPC         : %s\n", *rpcURL)
        if *genOnly {
                fmt.Printf("  Mode        : Generate Only (tanpa cek saldo)\n")
        } else {
                fmt.Printf("  Rate Limit  : %d ms/worker\n", *rateMs)
                fmt.Printf("  Timeout     : %d detik\n", *timeoutS)
                fmt.Printf("  Show All    : %v\n", *showAll)
                fmt.Printf("  Output File : %s\n", *outputFile)
        }
        fmt.Println()

        // Setup context dengan cancel (Ctrl+C)
        ctx, cancel := context.WithCancel(context.Background())
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        go func() {
                <-sigCh
                fmt.Println("\n[!] Diterima sinyal berhenti, menyelesaikan task...")
                cancel()
        }()

        // ===== MODE: Generate Only =====
        if *genOnly {
                runGenerateOnly(ctx, startIndex, endIndex, *showAll)
                return
        }

        // ===== MODE: Generate + Check =====
        runScanMode(ctx, cancel, startIndex, endIndex, *rpcURL, *workers, *rateMs, *timeoutS, *showAll, *outputFile)
}

// runGenerateOnly hanya generate dan print address tanpa cek saldo
func runGenerateOnly(ctx context.Context, startIndex, endIndex *big.Int, _ bool) {
        fmt.Println("[*] Mode: Generate Only")
        fmt.Println("─────────────────────────────────────────────────────────────────────────")
        fmt.Printf("%-6s  %-64s  %-42s\n", "No.", "Private Key", "Address")
        fmt.Println("─────────────────────────────────────────────────────────────────────────")

        current := new(big.Int).Set(startIndex)
        num := 1

        for current.Cmp(endIndex) <= 0 {
                select {
                case <-ctx.Done():
                        return
                default:
                }

                w, err := wallet.FromIndex(current)
                if err == nil {
                        fmt.Printf("%-6d  %s  %s\n", num, w.PrivateKeyHex, w.Address.Hex())
                } else {
                        fmt.Printf("%-6d  ERROR: %v\n", num, err)
                }

                current.Add(current, big.NewInt(1))
                num++
        }
        fmt.Println("\n[✓] Selesai generate.")
}

// runScanMode menjalankan scan dengan checker paralel
func runScanMode(
        ctx context.Context,
        cancel context.CancelFunc,
        startIndex, endIndex *big.Int,
        rpcURL string,
        workers, rateMs, timeoutS int,
        showAll bool,
        outputFile string,
) {
        cfg := checker.Config{
                RPCURL:      rpcURL,
                Workers:     workers,
                BatchSize:   workers * 2,
                RateLimit:   time.Duration(rateMs) * time.Millisecond,
                Timeout:     time.Duration(timeoutS) * time.Second,
                ShowAll:     showAll,
                SaveResults: true,
        }

        scanner := checker.NewScanner(cfg)

        // Buka file output
        var outFile *os.File
        var writer *bufio.Writer
        var fileMu sync.Mutex

        outFile, err := os.OpenFile(outputFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
        if err != nil {
                fmt.Fprintf(os.Stderr, "WARNING: Tidak bisa buka file output: %v\n", err)
        } else {
                defer outFile.Close()
                writer = bufio.NewWriterSize(outFile, 65536)
                defer writer.Flush()
                // Tulis header jika file baru
                stat, _ := outFile.Stat()
                if stat.Size() == 0 {
                        fmt.Fprintf(writer, "# ETH Wallet Scanner — Found Wallets\n")
                        fmt.Fprintf(writer, "# Tanggal: %s\n", time.Now().Format("2006-01-02 15:04:05"))
                        fmt.Fprintf(writer, "# Format: INDEX | PRIVATE_KEY | ADDRESS | BALANCE_ETH\n\n")
                }
        }

        resultCh := make(chan checker.Result, workers*4)

        // Goroutine untuk mencetak progress
        startTime := time.Now()
        progressDone := make(chan struct{})
        go func() {
                defer close(progressDone)
                ticker := time.NewTicker(2 * time.Second)
                defer ticker.Stop()
                for {
                        select {
                        case <-ctx.Done():
                                return
                        case <-ticker.C:
                                checked, withFunds, errs := scanner.Stats()
                                elapsed := time.Since(startTime).Seconds()
                                speed := float64(checked) / elapsed
                                fmt.Printf("\r[~] Checked: %6d | Dengan Saldo: %d | Error: %d | Speed: %.1f w/s   ",
                                        checked, withFunds, errs, speed)
                        }
                }
        }()

        // Goroutine untuk process hasil
        var resultWg sync.WaitGroup
        resultWg.Add(1)
        go func() {
                defer resultWg.Done()
                for res := range resultCh {
                        processResult(res, showAll, writer, outFile, &fileMu)
                }
        }()

        fmt.Printf("[*] Memulai scan...\n\n")

        // Jalankan scan (blocking sampai selesai atau ctx cancel)
        scanner.Run(ctx, startIndex, endIndex, resultCh)
        close(resultCh)
        resultWg.Wait()

        cancel()
        <-progressDone

        // Final stats
        elapsed := time.Since(startTime)
        checked, withFunds, errs := scanner.Stats()
        speed := float64(checked) / elapsed.Seconds()

        fmt.Printf("\n\n═══════════════════════════════════════\n")
        fmt.Printf("  HASIL SCAN\n")
        fmt.Printf("═══════════════════════════════════════\n")
        fmt.Printf("  Total Dicek    : %d wallet\n", checked)
        fmt.Printf("  Ada Saldo      : %d wallet\n", withFunds)
        fmt.Printf("  Error          : %d\n", errs)
        fmt.Printf("  Durasi         : %s\n", elapsed.Round(time.Millisecond))
        fmt.Printf("  Kecepatan      : %.1f wallet/detik\n", speed)
        if outFile != nil && withFunds > 0 {
                fmt.Printf("  Output         : %s\n", outputFile)
        }
        fmt.Printf("═══════════════════════════════════════\n")
}

// wei to ETH
var weiPerEth = new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

func weiToEth(wei *big.Int) string {
        if wei == nil || wei.Sign() == 0 {
                return "0"
        }
        f := new(big.Float).SetInt(wei)
        eth := new(big.Float).Quo(f, weiPerEth)
        return eth.Text('f', 8)
}

func processResult(
        res checker.Result,
        showAll bool,
        writer *bufio.Writer,
        outFile *os.File,
        mu *sync.Mutex,
) {
        if res.Error != nil {
                if showAll {
                        fmt.Printf("\n  [ERROR] %v\n", res.Error)
                }
                return
        }

        w := res.Wallet
        ethBalance := weiToEth(res.Balance)
        hasBalance := res.Balance != nil && res.Balance.Sign() > 0

        if showAll {
                status := "  "
                if hasBalance {
                        status = "💰"
                }
                fmt.Printf("\n%s [%s] %s | Balance: %s ETH\n",
                        status, w.PrivateKeyHex, w.Address.Hex(), ethBalance)
        } else if hasBalance {
                fmt.Printf("\n💰 [%s] %s | Balance: %s ETH\n",
                        w.PrivateKeyHex, w.Address.Hex(), ethBalance)
        }

        // Simpan ke file jika ada saldo
        if hasBalance && writer != nil {
                mu.Lock()
                fmt.Fprintf(writer, "%s | %s | %s | %s ETH\n",
                        w.Index.String(), w.PrivateKeyHex, w.Address.Hex(), ethBalance)
                writer.Flush()
                mu.Unlock()
        }
}
