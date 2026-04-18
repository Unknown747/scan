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
		startHex   = flag.String("start", "1", "Index awal")
		endHex     = flag.String("end", "1000", "Index akhir")
		workers    = flag.Int("workers", runtime.NumCPU()*2, "Jumlah goroutine paralel")
		batchSize  = flag.Int("batch", 20, "Wallet per satu batch RPC request")
		rpcURL     = flag.String("rpc", "https://eth.llamarpc.com", "Ethereum RPC endpoint")
		rateMs     = flag.Int("rate", 300, "Jeda antar batch per worker (milidetik)")
		timeoutS   = flag.Int("timeout", 15, "HTTP timeout (detik)")
		outputFile = flag.String("output", "found_wallets.txt", "File simpan wallet bersaldo")
		genOnly    = flag.Bool("gen", false, "Generate address saja tanpa cek saldo")
	)
	flag.Parse()

	fmt.Print(banner)

	startIndex := parseIndex(*startHex, "start")
	endIndex := parseIndex(*endHex, "end")

	if startIndex.Cmp(endIndex) > 0 {
		fmt.Fprintln(os.Stderr, "ERROR: start harus <= end")
		os.Exit(1)
	}

	total := new(big.Int).Sub(endIndex, startIndex)
	total.Add(total, big.NewInt(1))

	fmt.Printf("  Start     : %s\n", startIndex.String())
	fmt.Printf("  End       : %s\n", endIndex.String())
	fmt.Printf("  Total     : %s wallet\n", total.String())
	fmt.Printf("  Workers   : %d goroutines\n", *workers)
	if !*genOnly {
		fmt.Printf("  Batch RPC : %d wallet/request\n", *batchSize)
		fmt.Printf("  RPC       : %s\n", *rpcURL)
	}
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n[!] Dihentikan. Menyimpan data...")
		cancel()
	}()

	if *genOnly {
		runGenerateOnly(ctx, startIndex, endIndex)
		return
	}

	runScanMode(ctx, cancel, startIndex, endIndex, *rpcURL, *workers, *batchSize, *rateMs, *timeoutS, *outputFile)
}

// parseIndex parsing index dari string hex (0x...) atau desimal
func parseIndex(s, name string) *big.Int {
	n, ok := new(big.Int).SetString(s, 0)
	if !ok || n.Sign() <= 0 {
		n, ok = new(big.Int).SetString(s, 10)
		if !ok || n.Sign() <= 0 {
			fmt.Fprintf(os.Stderr, "ERROR: %s tidak valid: %s\n", name, s)
			os.Exit(1)
		}
	}
	return n
}

// runGenerateOnly generate + cetak address tanpa cek saldo
// Menggunakan big.NewInt(1) sekali (reuse) untuk increment
func runGenerateOnly(ctx context.Context, startIndex, endIndex *big.Int) {
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")

	one := big.NewInt(1)              // reuse, tidak alokasi baru tiap loop
	current := new(big.Int).Set(startIndex)
	var count int64 = 1

	for current.Cmp(endIndex) <= 0 {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		w, err := wallet.FromIndex(current)
		if err == nil {
			fmt.Printf("Count : %-10d  Addrs : %s  Bal : 0\n", count, w.Address.Hex())
		} else {
			fmt.Printf("Count : %-10d  ERROR: %v\n", count, err)
		}

		current.Add(current, one)
		count++
	}

done:
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("\n[✓] Selesai. Total: %d wallet\n", count-1)
}

// runScanMode scan saldo secara paralel dengan batch RPC
func runScanMode(
	ctx context.Context,
	cancel context.CancelFunc,
	startIndex, endIndex *big.Int,
	rpcURL string,
	workers, batchSize, rateMs, timeoutS int,
	outputFile string,
) {
	cfg := checker.Config{
		RPCURL:     rpcURL,
		Workers:    workers,
		BatchSize:  batchSize,
		RateLimit:  time.Duration(rateMs) * time.Millisecond,
		Timeout:    time.Duration(timeoutS) * time.Second,
		MaxRetries: 3,
	}

	scanner := checker.NewScanner(cfg)

	// File output dengan buffered writer 64KB untuk minimalkan syscall write
	var outFile *os.File
	var writer *bufio.Writer
	var fileMu sync.Mutex

	if f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Tidak bisa buka file output: %v\n", err)
	} else {
		outFile = f
		defer outFile.Close()
		writer = bufio.NewWriterSize(outFile, 65536) // 64KB buffer
		defer writer.Flush()
		if stat, _ := outFile.Stat(); stat.Size() == 0 {
			fmt.Fprintf(writer, "# ETH Wallet Scanner — Found Wallets\n")
			fmt.Fprintf(writer, "# Tanggal: %s\n", time.Now().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(writer, "# Format: COUNT | PRIVATE_KEY | ADDRESS | BALANCE_ETH\n\n")
		}
	}

	// Buffered result channel: workers*batchSize*2 agar producer tidak blocking
	resultCh := make(chan checker.Result, workers*batchSize*2)

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	startTime := time.Now()
	var displayCount atomic.Int64

	// Goroutine tunggal untuk print + file write (menghindari print race)
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		for res := range resultCh {
			n := displayCount.Add(1)
			printResult(n, res, writer, &fileMu)
		}
	}()

	// Jalankan scan (blocking sampai selesai atau ctx cancel)
	scanner.Run(ctx, startIndex, endIndex, resultCh)
	close(resultCh)
	resultWg.Wait()
	cancel()

	elapsed := time.Since(startTime)
	checked, withFunds, errs := scanner.Stats()

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("\n  Total Dicek  : %d\n", checked)
	fmt.Printf("  Ada Saldo    : %d wallet\n", withFunds)
	fmt.Printf("  Error        : %d\n", errs)
	fmt.Printf("  Durasi       : %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  Kecepatan    : %.1f wallet/detik\n", float64(checked)/elapsed.Seconds())
	if outFile != nil && withFunds > 0 {
		fmt.Printf("  Tersimpan di : %s\n", outputFile)
	}
}

// weiPerEth untuk konversi Wei → ETH (dihitung sekali, package-level)
var weiPerEth = new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

func weiToEth(wei *big.Int) string {
	if wei == nil || wei.Sign() == 0 {
		return "0"
	}
	eth := new(big.Float).Quo(new(big.Float).SetInt(wei), weiPerEth)
	return eth.Text('f', 8)
}

func printResult(count int64, res checker.Result, writer *bufio.Writer, mu *sync.Mutex) {
	if res.Wallet == nil {
		fmt.Printf("Count : %-10d  Addrs : %-42s  Bal : ERROR\n", count, "-")
		return
	}

	addr := res.Wallet.Address.Hex()

	if res.Error != nil {
		fmt.Printf("Count : %-10d  Addrs : %s  Bal : ERR\n", count, addr)
		return
	}

	ethBalance := weiToEth(res.Balance)
	hasBalance := res.Balance != nil && res.Balance.Sign() > 0

	if hasBalance {
		fmt.Printf("Count : %-10d  Addrs : %s  Bal : %s  <<< FOUND!\n", count, addr, ethBalance)
	} else {
		fmt.Printf("Count : %-10d  Addrs : %s  Bal : 0\n", count, addr)
	}

	// Tulis ke file hanya jika ada saldo (buffered 64KB, flush hanya saat found)
	if hasBalance && writer != nil {
		mu.Lock()
		fmt.Fprintf(writer, "%d | %s | %s | %s ETH\n",
			count, res.Wallet.PrivateKeyHex, addr, ethBalance)
		writer.Flush()
		mu.Unlock()
	}
}
