package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"eth-wallet-scanner/wallet"
)

// Result menyimpan hasil pengecekan satu wallet
type Result struct {
	Wallet  *wallet.Wallet
	Balance *big.Int
	Error   error
}

// Config konfigurasi checker
type Config struct {
	RPCURL     string
	Workers    int
	BatchSize  int           // jumlah wallet per satu HTTP batch RPC request
	RateLimit  time.Duration // jeda antar batch per worker
	Timeout    time.Duration
	MaxRetries int
}

// rpcReq format JSON-RPC 2.0 single request
type rpcReq struct {
	JSONRPC string    `json:"jsonrpc"`
	Method  string    `json:"method"`
	Params  [2]string `json:"params"`
	ID      int       `json:"id"`
}

// rpcResp format respons JSON-RPC
type rpcResp struct {
	ID     int       `json:"id"`
	Result string    `json:"result"`
	Error  *rpcError `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// bufPool — sync.Pool untuk reuse bytes.Buffer (mengurangi alokasi memori & GC pressure)
var bufPool = sync.Pool{
	New: func() interface{} { return new(bytes.Buffer) },
}

// reqSlicePool — sync.Pool untuk reuse slice []rpcReq
var reqSlicePool = sync.Pool{
	New: func() interface{} { return &[]rpcReq{} },
}

// parseHexBalance mengurai string hex saldo dari RPC menjadi *big.Int
func parseHexBalance(hexStr string) *big.Int {
	if len(hexStr) > 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	result := new(big.Int)
	if hexStr != "" {
		result.SetString(hexStr, 16)
	}
	return result
}

// getSingleBalance — satu request, satu wallet (fallback / individual retry)
func getSingleBalance(ctx context.Context, client *http.Client, rpcURL string, addr string) (*big.Int, error) {
	req := rpcReq{
		JSONRPC: "2.0",
		Method:  "eth_getBalance",
		Params:  [2]string{addr, "latest"},
		ID:      1,
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(req); err != nil {
		bufPool.Put(buf)
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, buf)
	if err != nil {
		bufPool.Put(buf)
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	bufPool.Put(buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decBuf := bufPool.Get().(*bytes.Buffer)
	decBuf.Reset()
	_, err = decBuf.ReadFrom(resp.Body)
	if err != nil {
		bufPool.Put(decBuf)
		return nil, err
	}

	var rpcR rpcResp
	err = json.Unmarshal(decBuf.Bytes(), &rpcR)
	bufPool.Put(decBuf)
	if err != nil {
		return nil, fmt.Errorf("decode single response: %w", err)
	}
	if rpcR.Error != nil {
		return nil, fmt.Errorf("RPC %d: %s", rpcR.Error.Code, rpcR.Error.Message)
	}
	return parseHexBalance(rpcR.Result), nil
}

// getSingleBalanceWithRetry — retry getSingleBalance dengan exponential backoff
func getSingleBalanceWithRetry(ctx context.Context, client *http.Client, rpcURL, addr string, maxRetries int) (*big.Int, error) {
	var lastErr error
	backoff := 300 * time.Millisecond
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		bal, err := getSingleBalance(ctx, client, rpcURL, addr)
		if err == nil {
			return bal, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("gagal setelah %d retry: %w", maxRetries, lastErr)
}

// batchGetBalance mengirim satu HTTP request berisi BANYAK eth_getBalance (batch JSON-RPC 2.0)
// Mengembalikan slice []*big.Int dengan panjang = len(wallets)
// Elemen nil berarti item tersebut gagal di level batch (akan di-fallback ke individual)
func batchGetBalance(ctx context.Context, client *http.Client, rpcURL string, wallets []*wallet.Wallet) ([]*big.Int, error) {
	n := len(wallets)

	// Ambil slice dari pool, resize jika perlu
	reqPtr := reqSlicePool.Get().(*[]rpcReq)
	reqs := (*reqPtr)[:0]
	if cap(reqs) < n {
		reqs = make([]rpcReq, n)
	} else {
		reqs = reqs[:n]
	}
	for i, w := range wallets {
		reqs[i] = rpcReq{
			JSONRPC: "2.0",
			Method:  "eth_getBalance",
			Params:  [2]string{w.Address.Hex(), "latest"},
			ID:      i,
		}
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(reqs); err != nil {
		bufPool.Put(buf)
		*reqPtr = reqs
		reqSlicePool.Put(reqPtr)
		return nil, fmt.Errorf("encode batch: %w", err)
	}
	*reqPtr = reqs
	reqSlicePool.Put(reqPtr)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, buf)
	if err != nil {
		bufPool.Put(buf)
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	bufPool.Put(buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	decBuf := bufPool.Get().(*bytes.Buffer)
	decBuf.Reset()
	if _, err := decBuf.ReadFrom(resp.Body); err != nil {
		bufPool.Put(decBuf)
		return nil, fmt.Errorf("baca response: %w", err)
	}

	// Coba decode sebagai array (batch response)
	var responses []rpcResp
	if err := json.Unmarshal(decBuf.Bytes(), &responses); err != nil {
		bufPool.Put(decBuf)
		// RPC tidak mendukung batch → kembalikan nil agar caller fallback ke individual
		return nil, fmt.Errorf("batch tidak didukung RPC: %w", err)
	}
	bufPool.Put(decBuf)

	// Inisialisasi results: semua nil dulu (belum ada data)
	results := make([]*big.Int, n)

	for _, r := range responses {
		if r.ID < 0 || r.ID >= n {
			continue
		}
		if r.Error != nil {
			// item gagal di level RPC (rate limit, dll) — biarkan nil untuk fallback
			continue
		}
		results[r.ID] = parseHexBalance(r.Result)
	}

	return results, nil
}

// checkBatchWithFallback — cek saldo batch, wallet yang nil di-fallback ke individual retry
func checkBatchWithFallback(
	ctx context.Context,
	client *http.Client,
	rpcURL string,
	wallets []*wallet.Wallet,
	maxRetries int,
) []*big.Int {
	n := len(wallets)
	results := make([]*big.Int, n)

	// Coba batch dulu
	batchResults, batchErr := batchGetBalance(ctx, client, rpcURL, wallets)

	if batchErr == nil {
		// Batch berhasil — salin yang berhasil, fallback yang nil
		for i, bal := range batchResults {
			if bal != nil {
				results[i] = bal
			}
			// nil → akan di-fallback di bawah
		}
	}
	// Jika batchErr != nil → semua masih nil, fallback individual

	// Fallback: retry individual untuk yang masih nil
	for i, w := range wallets {
		if results[i] != nil {
			continue
		}
		select {
		case <-ctx.Done():
			// Isi sisa dengan zero agar tidak ada panic
			for j := i; j < n; j++ {
				if results[j] == nil {
					results[j] = new(big.Int)
				}
			}
			return results
		default:
		}
		bal, err := getSingleBalanceWithRetry(ctx, client, rpcURL, w.Address.Hex(), maxRetries)
		if err != nil {
			results[i] = nil // tetap nil → caller tandai error
		} else {
			results[i] = bal
		}
	}

	return results
}

// Scanner menangani proses scan wallet secara paralel
type Scanner struct {
	config    Config
	client    *http.Client
	checked   atomic.Int64
	withFunds atomic.Int64
	errors    atomic.Int64
}

// NewScanner membuat Scanner baru dengan HTTP transport yang sudah dioptimasi
func NewScanner(cfg Config) *Scanner {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}

	transport := &http.Transport{
		// Connection pool: cukup koneksi idle agar tidak buka-tutup tiap request
		MaxIdleConns:        cfg.Workers * 4,
		MaxIdleConnsPerHost: cfg.Workers * 4,
		IdleConnTimeout:     120 * time.Second,
		// Non-aktifkan kompresi agar tidak ada overhead decompress
		DisableCompression: true,
		// Paksa HTTP/2 untuk multiplexing di satu koneksi
		ForceAttemptHTTP2: true,
	}

	return &Scanner{
		config: cfg,
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

// Stats mengembalikan statistik saat ini (thread-safe via atomic)
func (s *Scanner) Stats() (checked, withFunds, errors int64) {
	return s.checked.Load(), s.withFunds.Load(), s.errors.Load()
}

// Run menjalankan scan dari startIndex sampai endIndex (inklusif)
// Producer mengemas index ke dalam batch, worker memproses batch secara paralel
func (s *Scanner) Run(ctx context.Context, startIndex, endIndex *big.Int, resultCh chan<- Result) {
	batchSize := s.config.BatchSize
	// Channel berisi batch — buffer cukup besar agar producer tidak blocking
	batchCh := make(chan []*big.Int, s.config.Workers*3)

	// Spawn worker pool
	var wg sync.WaitGroup
	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(ctx, batchCh, resultCh)
		}()
	}

	// Producer: kumpulkan index menjadi batch, kirim ke batchCh
	// Reuse big.NewInt(1) agar tidak alokasi baru tiap iterasi (big.Int reuse)
	go func() {
		one := big.NewInt(1)              // dialokasi SEKALI, dipakai ulang terus
		current := new(big.Int).Set(startIndex)
		batch := make([]*big.Int, 0, batchSize)

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case batchCh <- batch:
				batch = make([]*big.Int, 0, batchSize)
				return true
			}
		}

		for current.Cmp(endIndex) <= 0 {
			select {
			case <-ctx.Done():
				close(batchCh)
				return
			default:
			}

			batch = append(batch, new(big.Int).Set(current))
			current.Add(current, one) // reuse one — tidak ada alokasi baru

			if len(batch) == batchSize {
				if !flush() {
					close(batchCh)
					return
				}
			}
		}

		flush()
		close(batchCh)
	}()

	wg.Wait()
}

// worker memproses satu batch: generate wallet + batch RPC (dengan fallback individual)
func (s *Scanner) worker(ctx context.Context, batchCh <-chan []*big.Int, resultCh chan<- Result) {
	// Rate limit per worker: jeda antar batch agar tidak spam RPC
	rateTicker := time.NewTicker(s.config.RateLimit)
	defer rateTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case indices, ok := <-batchCh:
			if !ok {
				return
			}

			// Tunggu rate limiter sebelum kirim ke RPC
			select {
			case <-rateTicker.C:
			case <-ctx.Done():
				return
			}

			// Generate semua wallet dari index dalam batch
			wallets := make([]*wallet.Wallet, 0, len(indices))
			for _, idx := range indices {
				w, err := wallet.FromIndex(idx)
				if err != nil {
					s.errors.Add(1)
					resultCh <- Result{Error: fmt.Errorf("index %s: %w", idx.String(), err)}
					continue
				}
				wallets = append(wallets, w)
			}

			if len(wallets) == 0 {
				continue
			}

			// Cek saldo: coba batch dulu, fallback ke individual jika perlu
			balances := checkBatchWithFallback(ctx, s.client, s.config.RPCURL, wallets, s.config.MaxRetries)
			s.checked.Add(int64(len(wallets)))

			for i, w := range wallets {
				bal := balances[i]
				if bal == nil {
					// Individual retry pun gagal
					s.errors.Add(1)
					resultCh <- Result{Wallet: w, Error: fmt.Errorf("gagal cek saldo %s", w.Address.Hex())}
					continue
				}
				if bal.Sign() > 0 {
					s.withFunds.Add(1)
				}
				resultCh <- Result{Wallet: w, Balance: bal}
			}
		}
	}
}
