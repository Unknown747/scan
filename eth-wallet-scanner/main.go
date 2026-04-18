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
	"sync/atomic"
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
	var (
		startHex   = flag.String("start", "1", "Index awal (hex atau desimal), default: 1")
		endHex     = flag.String("end", "1000", "Index akhir (hex atau desimal), default: 1000")
		workers    = flag.Int("workers", runtime.NumCPU()*2, "Jumlah goroutine worker paralel")
		rpcURL     = flag.String("rpc", "https://eth.llamarpc.com", "Ethereum RPC endpoint")
		rateMs     = flag.Int("rate", 100, "Jeda antar request per worker (milidetik)")
		timeoutS   = flag.Int("timeout", 10, "HTTP timeout per request (detik)")
		outputFile = flag.String("output", "found_wallets.txt", "File output untuk wallet yang punya saldo")
		genOnly    = flag.Bool("gen", false, "Hanya generate address tanpa cek saldo")
	)
	flag.Parse()

	fmt.Print(banner)

	// Parse start index
	startIndex, ok := new(big.Int).SetString(*startHex, 0)
	if !ok || startIndex.Sign() <= 0 {
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

	fmt.Printf("  Start   : %s\n", startIndex.String())
	fmt.Printf("  End     : %s\n", endIndex.String())
	fmt.Printf("  Total   : %s wallet\n", totalWallets.String())
	fmt.Printf("  Workers : %d goroutines\n", *workers)
	if !*genOnly {
		fmt.Printf("  RPC     : %s\n", *rpcURL)
	}
	fmt.Println()

	// Setup context dengan cancel (Ctrl+C)
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[!] Dihentikan.")
		cancel()
	}()

	if *genOnly {
		runGenerateOnly(ctx, startIndex, endIndex)
		return
	}

	runScanMode(ctx, cancel, startIndex, endIndex, *rpcURL, *workers, *rateMs, *timeoutS, *outputFile)
}

// runGenerateOnly hanya generate dan print address tanpa cek saldo
func runGenerateOnly(ctx context.Context, startIndex, endIndex *big.Int) {
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")

	current := new(big.Int).Set(startIndex)
	var count int64 = 1

	for current.Cmp(endIndex) <= 0 {
		select {
		case <-ctx.Done():
			return
		default:
		}

		w, err := wallet.FromIndex(current)
		if err == nil {
			fmt.Printf("Count : %-10d  Addrs : %s  Bal : 0\n", count, w.Address.Hex())
		} else {
			fmt.Printf("Count : %-10d  ERROR: %v\n", count, err)
		}

		current.Add(current, big.NewInt(1))
		count++
	}

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("\n[✓] Selesai. Total generate: %d wallet\n", count-1)
}

// runScanMode menjalankan scan dengan checker paralel
func runScanMode(
	ctx context.Context,
	cancel context.CancelFunc,
	startIndex, endIndex *big.Int,
	rpcURL string,
	workers, rateMs, timeoutS int,
	outputFile string,
) {
	cfg := checker.Config{
		RPCURL:      rpcURL,
		Workers:     workers,
		BatchSize:   workers * 2,
		RateLimit:   time.Duration(rateMs) * time.Millisecond,
		Timeout:     time.Duration(timeoutS) * time.Second,
		ShowAll:     true,
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
		stat, _ := outFile.Stat()
		if stat.Size() == 0 {
			fmt.Fprintf(writer, "# ETH Wallet Scanner — Found Wallets\n")
			fmt.Fprintf(writer, "# Tanggal: %s\n", time.Now().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(writer, "# Format: COUNT | PRIVATE_KEY | ADDRESS | BALANCE_ETH\n\n")
		}
	}

	resultCh := make(chan checker.Result, workers*4)

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")

	startTime := time.Now()
	var displayCount atomic.Int64

	// Goroutine untuk process dan tampilkan hasil
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for res := range resultCh {
			n := displayCount.Add(1)
			printResult(n, res, writer, &fileMu)
		}
	}()

	// Jalankan scan
	scanner.Run(ctx, startIndex, endIndex, resultCh)
	close(resultCh)
	resultWg.Wait()

	cancel()

	// Final stats
	elapsed := time.Since(startTime)
	checked, withFunds, errs := scanner.Stats()
	speed := float64(checked) / elapsed.Seconds()

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("\n  Total Dicek  : %d\n", checked)
	fmt.Printf("  Ada Saldo    : %d wallet\n", withFunds)
	fmt.Printf("  Error        : %d\n", errs)
	fmt.Printf("  Durasi       : %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Kecepatan    : %.1f wallet/detik\n", speed)
	if outFile != nil && withFunds > 0 {
		fmt.Printf("  Tersimpan di : %s\n", outputFile)
	}
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

func printResult(
	count int64,
	res checker.Result,
	writer *bufio.Writer,
	mu *sync.Mutex,
) {
	if res.Error != nil {
		fmt.Printf("Count : %-10d  Addrs : %-42s  Bal : ERROR\n", count, "-")
		return
	}

	w := res.Wallet
	ethBalance := weiToEth(res.Balance)
	hasBalance := res.Balance != nil && res.Balance.Sign() > 0

	if hasBalance {
		// Tandai khusus jika ada saldo
		fmt.Printf("Count : %-10d  Addrs : %s  Bal : %s  <<< FOUND!\n",
			count, w.Address.Hex(), ethBalance)
	} else {
		fmt.Printf("Count : %-10d  Addrs : %s  Bal : 0\n",
			count, w.Address.Hex())
	}

	// Simpan ke file jika ada saldo
	if hasBalance && writer != nil {
		mu.Lock()
		fmt.Fprintf(writer, "%d | %s | %s | %s ETH\n",
			count, w.PrivateKeyHex, w.Address.Hex(), ethBalance)
		writer.Flush()
		mu.Unlock()
	}
}
