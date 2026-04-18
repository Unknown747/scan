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

	"github.com/ethereum/go-ethereum/common"

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
	RPCURL      string
	Workers     int
	BatchSize   int
	RateLimit   time.Duration // jeda antar request per worker
	Timeout     time.Duration
	ShowAll     bool // jika false, hanya tampilkan yang ada saldo
	SaveResults bool
}

// DefaultConfig konfigurasi default yang sudah optimal
func DefaultConfig() Config {
	return Config{
		RPCURL:      "https://eth.llamarpc.com",
		Workers:     10,
		BatchSize:   50,
		RateLimit:   100 * time.Millisecond,
		Timeout:     10 * time.Second,
		ShowAll:     false,
		SaveResults: true,
	}
}

// rpcRequest format JSON-RPC 2.0
type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// rpcResponse format respons JSON-RPC
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  string          `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// getBalance mengambil saldo satu address via eth_getBalance
func getBalance(ctx context.Context, client *http.Client, rpcURL string, address common.Address) (*big.Int, error) {
	reqBody := rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBalance",
		Params:  []interface{}{address.Hex(), "latest"},
		ID:      1,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Parse hex balance (dalam Wei)
	balHex := rpcResp.Result
	if len(balHex) >= 2 && balHex[:2] == "0x" {
		balHex = balHex[2:]
	}
	if balHex == "" {
		balHex = "0"
	}

	balance := new(big.Int)
	balance.SetString(balHex, 16)
	return balance, nil
}

// Scanner menangani proses scan wallet secara paralel
type Scanner struct {
	config    Config
	client    *http.Client
	checked   atomic.Int64
	withFunds atomic.Int64
	errors    atomic.Int64
}

// NewScanner membuat Scanner baru
func NewScanner(cfg Config) *Scanner {
	transport := &http.Transport{
		MaxIdleConns:        cfg.Workers * 2,
		MaxIdleConnsPerHost: cfg.Workers * 2,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}

	return &Scanner{
		config: cfg,
		client: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
	}
}

// Stats mengembalikan statistik saat ini
func (s *Scanner) Stats() (checked, withFunds, errors int64) {
	return s.checked.Load(), s.withFunds.Load(), s.errors.Load()
}

// Run menjalankan scan dari startIndex sampai endIndex (inklusif)
// Menggunakan worker pool untuk paralelisme
func (s *Scanner) Run(
	ctx context.Context,
	startIndex, endIndex *big.Int,
	resultCh chan<- Result,
) {
	// Channel untuk mendistribusikan index ke workers
	indexCh := make(chan *big.Int, s.config.Workers*2)

	var wg sync.WaitGroup

	// Spawn workers
	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			s.worker(ctx, workerID, indexCh, resultCh)
		}(i)
	}

	// Produce index ke channel
	go func() {
		current := new(big.Int).Set(startIndex)
		for current.Cmp(endIndex) <= 0 {
			select {
			case <-ctx.Done():
				close(indexCh)
				return
			case indexCh <- new(big.Int).Set(current):
				current.Add(current, big.NewInt(1))
			}
		}
		close(indexCh)
	}()

	wg.Wait()
}

// worker memproses index dari channel dan mengirim hasil ke resultCh
func (s *Scanner) worker(
	ctx context.Context,
	id int,
	indexCh <-chan *big.Int,
	resultCh chan<- Result,
) {
	rateTicker := time.NewTicker(s.config.RateLimit)
	defer rateTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case idx, ok := <-indexCh:
			if !ok {
				return
			}

			// Rate limiting per worker
			select {
			case <-rateTicker.C:
			case <-ctx.Done():
				return
			}

			w, err := wallet.FromIndex(idx)
			if err != nil {
				s.errors.Add(1)
				resultCh <- Result{Wallet: nil, Error: fmt.Errorf("index %s: %w", idx.String(), err)}
				continue
			}

			balance, err := getBalance(ctx, s.client, s.config.RPCURL, w.Address)
			s.checked.Add(1)

			if err != nil {
				s.errors.Add(1)
				resultCh <- Result{Wallet: w, Balance: nil, Error: err}
				continue
			}

			if balance.Sign() > 0 {
				s.withFunds.Add(1)
			}

			resultCh <- Result{Wallet: w, Balance: balance, Error: nil}
		}
	}
}
