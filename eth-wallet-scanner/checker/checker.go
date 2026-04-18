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

type Result struct {
	Wallet  *wallet.Wallet
	Balance *big.Int
	Error   error
}

type Config struct {
	RPCURLs    []string
	Workers    int
	BatchSize  int
	RateLimit  time.Duration
	Timeout    time.Duration
	MaxRetries int
}

type rpcReq struct {
	JSONRPC string    `json:"jsonrpc"`
	Method  string    `json:"method"`
	Params  [2]string `json:"params"`
	ID      int       `json:"id"`
}

type rpcResp struct {
	ID     int       `json:"id"`
	Result string    `json:"result"`
	Error  *rpcError `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcNode tracks health of a single RPC endpoint.
type rpcNode struct {
	url       string
	errScore  atomic.Int64
	coolUntil atomic.Int64 // unix nano; skip until this time
}

func (r *rpcNode) recordError() {
	score := r.errScore.Add(1)
	if score >= 5 {
		r.coolUntil.Store(time.Now().Add(30 * time.Second).UnixNano())
		r.errScore.Store(0)
	}
}

func (r *rpcNode) recordSuccess() {
	if r.errScore.Load() > 0 {
		r.errScore.Add(-1)
	}
	r.coolUntil.Store(0)
}

func (r *rpcNode) available() bool {
	cu := r.coolUntil.Load()
	return cu == 0 || time.Now().UnixNano() >= cu
}

var bufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}
var reqSlicePool = sync.Pool{New: func() interface{} { return &[]rpcReq{} }}

func parseHexBalance(hexStr string) *big.Int {
	if len(hexStr) > 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	n := new(big.Int)
	if hexStr != "" {
		n.SetString(hexStr, 16)
	}
	return n
}

func getSingleBalance(ctx context.Context, client *http.Client, rpcURL, addr string) (*big.Int, error) {
	req := rpcReq{JSONRPC: "2.0", Method: "eth_getBalance", Params: [2]string{addr, "latest"}, ID: 1}

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

	var r rpcResp
	err = json.Unmarshal(decBuf.Bytes(), &r)
	bufPool.Put(decBuf)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if r.Error != nil {
		return nil, fmt.Errorf("RPC %d: %s", r.Error.Code, r.Error.Message)
	}
	return parseHexBalance(r.Result), nil
}

func batchGetBalance(ctx context.Context, client *http.Client, node *rpcNode, wallets []*wallet.Wallet) ([]*big.Int, error) {
	n := len(wallets)
	reqPtr := reqSlicePool.Get().(*[]rpcReq)
	reqs := (*reqPtr)[:0]
	if cap(reqs) < n {
		reqs = make([]rpcReq, n)
	} else {
		reqs = reqs[:n]
	}
	for i, w := range wallets {
		reqs[i] = rpcReq{JSONRPC: "2.0", Method: "eth_getBalance", Params: [2]string{w.Address.Hex(), "latest"}, ID: i}
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, node.url, buf)
	if err != nil {
		bufPool.Put(buf)
		node.recordError()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	bufPool.Put(buf)
	if err != nil {
		node.recordError()
		return nil, err
	}
	defer resp.Body.Close()

	decBuf := bufPool.Get().(*bytes.Buffer)
	decBuf.Reset()
	if _, err := decBuf.ReadFrom(resp.Body); err != nil {
		bufPool.Put(decBuf)
		node.recordError()
		return nil, fmt.Errorf("read response: %w", err)
	}

	var responses []rpcResp
	if err := json.Unmarshal(decBuf.Bytes(), &responses); err != nil {
		bufPool.Put(decBuf)
		node.recordError()
		return nil, fmt.Errorf("batch not supported: %w", err)
	}
	bufPool.Put(decBuf)

	results := make([]*big.Int, n)
	for _, r := range responses {
		if r.ID >= 0 && r.ID < n && r.Error == nil {
			results[r.ID] = parseHexBalance(r.Result)
		}
	}
	node.recordSuccess()
	return results, nil
}

type Scanner struct {
	config    Config
	client    *http.Client
	rpcNodes  []*rpcNode
	rpcIdx    atomic.Uint64
	checked   atomic.Int64
	withFunds atomic.Int64
	errors    atomic.Int64
	lastIdxMu sync.Mutex
	lastIdx   *big.Int
}

// pickNode selects the best available RPC node using round-robin,
// skipping nodes currently in cooldown.
func (s *Scanner) pickNode() *rpcNode {
	total := uint64(len(s.rpcNodes))
	start := s.rpcIdx.Add(1) % total
	for i := uint64(0); i < total; i++ {
		node := s.rpcNodes[(start+i)%total]
		if node.available() {
			return node
		}
	}
	// All in cooldown: pick the one whose cooldown expires soonest
	best := s.rpcNodes[0]
	for _, node := range s.rpcNodes[1:] {
		if node.coolUntil.Load() < best.coolUntil.Load() {
			best = node
		}
	}
	return best
}

func (s *Scanner) checkBatchWithFallback(ctx context.Context, wallets []*wallet.Wallet) []*big.Int {
	count := len(wallets)
	results := make([]*big.Int, count)

	// Try batch first
	node := s.pickNode()
	if batchResults, err := batchGetBalance(ctx, s.client, node, wallets); err == nil {
		copy(results, batchResults)
	}

	// Fallback: individual retry for any nil results
	backoff := 200 * time.Millisecond
	for i, w := range wallets {
		if results[i] != nil {
			continue
		}
		select {
		case <-ctx.Done():
			for j := i; j < count; j++ {
				if results[j] == nil {
					results[j] = new(big.Int)
				}
			}
			return results
		default:
		}

		for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					goto nextWallet
				case <-time.After(backoff):
				}
				backoff *= 2
				if backoff > 2*time.Second {
					backoff = 2 * time.Second
				}
			}
			n := s.pickNode()
			bal, err := getSingleBalance(ctx, s.client, n.url, w.Address.Hex())
			if err == nil {
				n.recordSuccess()
				results[i] = bal
				break
			}
			n.recordError()
		}
	nextWallet:
	}
	return results
}

func (s *Scanner) updateLastIdx(idx *big.Int) {
	s.lastIdxMu.Lock()
	if s.lastIdx == nil || idx.Cmp(s.lastIdx) > 0 {
		s.lastIdx = new(big.Int).Set(idx)
	}
	s.lastIdxMu.Unlock()
}

func (s *Scanner) LastIndex() *big.Int {
	s.lastIdxMu.Lock()
	defer s.lastIdxMu.Unlock()
	if s.lastIdx == nil {
		return nil
	}
	return new(big.Int).Set(s.lastIdx)
}

func NewScanner(cfg Config) *Scanner {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if len(cfg.RPCURLs) == 0 {
		cfg.RPCURLs = []string{"https://eth.llamarpc.com"}
	}

	nodes := make([]*rpcNode, len(cfg.RPCURLs))
	for i, url := range cfg.RPCURLs {
		nodes[i] = &rpcNode{url: url}
	}

	return &Scanner{
		config:   cfg,
		rpcNodes: nodes,
		client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        cfg.Workers * 4,
				MaxIdleConnsPerHost: cfg.Workers * 4,
				IdleConnTimeout:     120 * time.Second,
				DisableCompression:  true,
				ForceAttemptHTTP2:   true,
			},
		},
	}
}

func (s *Scanner) Stats() (checked, withFunds, errors int64) {
	return s.checked.Load(), s.withFunds.Load(), s.errors.Load()
}

func (s *Scanner) Run(ctx context.Context, startIndex, endIndex *big.Int, resultCh chan<- Result) {
	batchCh := make(chan []*big.Int, s.config.Workers*3)

	var wg sync.WaitGroup
	for i := 0; i < s.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.worker(ctx, batchCh, resultCh)
		}()
	}

	go func() {
		one := big.NewInt(1)
		current := new(big.Int).Set(startIndex)
		batch := make([]*big.Int, 0, s.config.BatchSize)

		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case batchCh <- batch:
				batch = make([]*big.Int, 0, s.config.BatchSize)
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
			current.Add(current, one)
			if len(batch) == s.config.BatchSize {
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

func (s *Scanner) worker(ctx context.Context, batchCh <-chan []*big.Int, resultCh chan<- Result) {
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
			select {
			case <-rateTicker.C:
			case <-ctx.Done():
				return
			}

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

			balances := s.checkBatchWithFallback(ctx, wallets)
			s.checked.Add(int64(len(wallets)))

			for i, w := range wallets {
				bal := balances[i]
				if bal == nil {
					s.errors.Add(1)
					resultCh <- Result{Wallet: w, Error: fmt.Errorf("balance check failed: %s", w.Address.Hex())}
					continue
				}
				if bal.Sign() > 0 {
					s.withFunds.Add(1)
				}
				resultCh <- Result{Wallet: w, Balance: bal}
			}

			s.updateLastIdx(indices[len(indices)-1])
		}
	}
}
