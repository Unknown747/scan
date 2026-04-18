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
	"strings"
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
		startHex   = flag.String("start", "1", "Index awal (diabaikan jika last_key.txt ada)")
		endHex     = flag.String("end", "1000", "Index akhir")
		workers    = flag.Int("workers", runtime.NumCPU()*2, "Jumlah goroutine paralel")
		batchSize  = flag.Int("batch", 20, "Wallet per satu batch RPC request")
		rpcURL     = flag.String("rpc", "https://eth.llamarpc.com", "Ethereum RPC endpoint")
		rateMs     = flag.Int("rate", 300, "Jeda antar batch per worker (milidetik)")
		timeoutS   = flag.Int("timeout", 15, "HTTP timeout (detik)")
		outputFile = flag.String("output", "found_wallets.txt", "File simpan wallet bersaldo")
		lastFile   = flag.String("last", "last_key.txt", "File simpan & resume index terakhir")
		genOnly    = flag.Bool("gen", false, "Generate address saja tanpa cek saldo")
	)
	flag.Parse()

	fmt.Print(banner)

	startIndex := parseIndex(*startHex, "start")
	endIndex := parseIndex(*endHex, "end")

	// Resume otomatis: baca last_key.txt, lanjut dari index+1 jika ada
	if !*genOnly {
		if resumed := readLastKey(*lastFile); resumed != nil {
			next := new(big.Int).Add(resumed, big.NewInt(1))
			if next.Cmp(startIndex) > 0 && next.Cmp(endIndex) <= 0 {
				fmt.Printf("  [Resume]  : lanjut dari index %s (last_key.txt)\n", next.String())
				startIndex = next
			} else if next.Cmp(endIndex) > 0 {
				fmt.Printf("  [Resume]  : index %s sudah melewati END (%s). Reset dari START.\n",
					resumed.String(), endIndex.String())
			}
		}
	}

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
		fmt.Printf("  Progress  : %s\n", *lastFile)
	}
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if *genOnly {
		go func() {
			<-sigCh
			fmt.Println("\n[!] Dihentikan.")
			cancel()
		}()
		runGenerateOnly(ctx, startIndex, endIndex)
		return
	}

	runScanMode(ctx, cancel, sigCh, startIndex, endIndex,
		*rpcURL, *workers, *batchSize, *rateMs, *timeoutS,
		*outputFile, *lastFile)
}

// ─── Resume helpers ────────────────────────────────────────────────────────

// readLastKey membaca index terakhir dari file. Mengembalikan nil jika tidak ada/invalid.
func readLastKey(path string) *big.Int {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // file belum ada — mulai baru
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return nil
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() <= 0 {
		return nil
	}
	return n
}

// saveLastKey menulis index saat ini ke file (overwrite)
func saveLastKey(path string, idx *big.Int) {
	if idx == nil {
		return
	}
	_ = os.WriteFile(path, []byte(idx.String()+"\n"), 0644)
}

// ─── parseIndex ─────────────────────────────────────────────────────────────

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

// ─── Generate-only mode ─────────────────────────────────────────────────────

func runGenerateOnly(ctx context.Context, startIndex, endIndex *big.Int) {
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")

	one := big.NewInt(1)
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

// ─── Scan mode ───────────────────────────────────────────────────────────────

func runScanMode(
	ctx context.Context,
	cancel context.CancelFunc,
	sigCh <-chan os.Signal,
	startIndex, endIndex *big.Int,
	rpcURL string,
	workers, batchSize, rateMs, timeoutS int,
	outputFile, lastFile string,
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

	// File output — buffered writer 64KB
	var outFile *os.File
	var writer *bufio.Writer
	var fileMu sync.Mutex

	if f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Tidak bisa buka file output: %v\n", err)
	} else {
		outFile = f
		defer outFile.Close()
		writer = bufio.NewWriterSize(outFile, 65536)
		defer writer.Flush()
		if stat, _ := outFile.Stat(); stat.Size() == 0 {
			fmt.Fprintf(writer, "# ETH Wallet Scanner — Found Wallets\n")
			fmt.Fprintf(writer, "# Tanggal: %s\n", time.Now().Format("2006-01-02 15:04:05"))
			fmt.Fprintf(writer, "# Format: COUNT | PRIVATE_KEY | ADDRESS | BALANCE_ETH\n\n")
		}
	}

	// Buffered result channel
	resultCh := make(chan checker.Result, workers*batchSize*2)

	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────")
	startTime := time.Now()
	var displayCount atomic.Int64

	// Signal handler: simpan progress sebelum keluar
	go func() {
		<-sigCh
		fmt.Println("\n[!] Dihentikan. Menyimpan progress...")
		if idx := scanner.LastIndex(); idx != nil {
			saveLastKey(lastFile, idx)
			fmt.Printf("[✓] Progress tersimpan di %s (index: %s)\n", lastFile, idx.String())
		}
		cancel()
	}()

	// Result processor — satu goroutine: print + tulis file + simpan progress tiap 100
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		var saveCounter int64
		for res := range resultCh {
			n := displayCount.Add(1)
			printResult(n, res, writer, &fileMu)

			// Simpan progress ke last_key.txt setiap 100 wallet
			saveCounter++
			if saveCounter%100 == 0 {
				if idx := scanner.LastIndex(); idx != nil {
					saveLastKey(lastFile, idx)
				}
			}
		}
	}()

	// Jalankan scan
	scanner.Run(ctx, startIndex, endIndex, resultCh)
	close(resultCh)
	resultWg.Wait()
	cancel()

	// Simpan progress akhir
	if idx := scanner.LastIndex(); idx != nil {
		saveLastKey(lastFile, idx)
	}

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
	if idx := scanner.LastIndex(); idx != nil {
		fmt.Printf("  Last Key     : %s → %s\n", lastFile, idx.String())
	}
}

// ─── Print & file writer ─────────────────────────────────────────────────────

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

	if hasBalance && writer != nil {
		mu.Lock()
		fmt.Fprintf(writer, "%d | %s | %s | %s ETH\n",
			count, res.Wallet.PrivateKeyHex, addr, ethBalance)
		writer.Flush()
		mu.Unlock()
	}
}
