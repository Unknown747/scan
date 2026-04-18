package wallet

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Wallet struct {
	PrivateKeyHex string
	Address       common.Address
	Index         *big.Int
}

var maxKey, _ = new(big.Int).SetString(
	"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364140", 16,
)

var keyBytesPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32)
		return &b
	},
}

func FromIndex(index *big.Int) (*Wallet, error) {
	if index.Sign() <= 0 {
		return nil, fmt.Errorf("index must be greater than 0")
	}
	if index.Cmp(maxKey) > 0 {
		return nil, fmt.Errorf("index exceeds secp256k1 max private key")
	}

	bPtr := keyBytesPool.Get().(*[]byte)
	keyBytes := *bPtr
	for i := range keyBytes {
		keyBytes[i] = 0
	}

	indexBytes := index.Bytes()
	copy(keyBytes[32-len(indexBytes):], indexBytes)

	privKey, err := crypto.ToECDSA(keyBytes)
	keyBytesPool.Put(bPtr)

	if err != nil {
		return nil, fmt.Errorf("failed to create private key: %w", err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to get ECDSA public key")
	}

	return &Wallet{
		PrivateKeyHex: fmt.Sprintf("%064x", index),
		Address:       crypto.PubkeyToAddress(*pubKey),
		Index:         new(big.Int).Set(index),
	}, nil
}
