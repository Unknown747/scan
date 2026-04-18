package wallet

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet mewakili satu wallet Ethereum
type Wallet struct {
	PrivateKeyHex string
	Address       common.Address
	Index         *big.Int
}

// maxKey adalah batas maksimum private key secp256k1 (konstan, tidak berubah)
var maxKey, _ = new(big.Int).SetString(
	"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364140", 16,
)

// keyBytesPool — sync.Pool untuk reuse buffer 32-byte saat derive private key
// Mengurangi tekanan GC karena keyBytes dialokasi jutaan kali saat scan
var keyBytesPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32)
		return &b
	},
}

// FromIndex menghasilkan wallet Ethereum dari index sequential
// Index 1 = private key 0x000...001
func FromIndex(index *big.Int) (*Wallet, error) {
	if index.Sign() <= 0 {
		return nil, fmt.Errorf("index harus lebih besar dari 0")
	}
	if index.Cmp(maxKey) > 0 {
		return nil, fmt.Errorf("index melebihi batas maksimum private key secp256k1")
	}

	// Ambil buffer dari pool, reset isinya ke nol
	bPtr := keyBytesPool.Get().(*[]byte)
	keyBytes := *bPtr
	for i := range keyBytes {
		keyBytes[i] = 0
	}

	// Salin bytes dari index (right-aligned dalam 32 bytes)
	indexBytes := index.Bytes()
	copy(keyBytes[32-len(indexBytes):], indexBytes)

	privKey, err := crypto.ToECDSA(keyBytes)

	// Kembalikan buffer ke pool sebelum return (tidak perlu lagi)
	keyBytesPool.Put(bPtr)

	if err != nil {
		return nil, fmt.Errorf("gagal membuat private key: %w", err)
	}

	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("gagal mendapatkan public key ECDSA")
	}

	return &Wallet{
		PrivateKeyHex: fmt.Sprintf("%064x", index),
		Address:       crypto.PubkeyToAddress(*pubKey),
		Index:         new(big.Int).Set(index),
	}, nil
}
