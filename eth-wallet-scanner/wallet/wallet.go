package wallet

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Wallet mewakili satu wallet Ethereum
type Wallet struct {
	PrivateKeyHex string
	Address       common.Address
	Index         *big.Int
}

// FromIndex menghasilkan wallet Ethereum dari index sequential (big.Int)
// Index 1 = private key 0x000...001
func FromIndex(index *big.Int) (*Wallet, error) {
	if index.Sign() <= 0 {
		return nil, fmt.Errorf("index harus lebih besar dari 0")
	}

	// Batas maksimum private key secp256k1
	maxKey, _ := new(big.Int).SetString(
		"FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364140",
		16,
	)
	if index.Cmp(maxKey) > 0 {
		return nil, fmt.Errorf("index melebihi batas maksimum private key secp256k1")
	}

	// Pad ke 32 bytes
	keyBytes := make([]byte, 32)
	indexBytes := index.Bytes()
	copy(keyBytes[32-len(indexBytes):], indexBytes)

	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("gagal membuat private key: %w", err)
	}

	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("gagal mendapatkan public key ECDSA")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	return &Wallet{
		PrivateKeyHex: fmt.Sprintf("%064x", index),
		Address:       address,
		Index:         new(big.Int).Set(index),
	}, nil
}

// FromIndexUint64 adalah versi uint64 untuk kemudahan penggunaan di range kecil
func FromIndexUint64(i uint64) (*Wallet, error) {
	return FromIndex(new(big.Int).SetUint64(i))
}
