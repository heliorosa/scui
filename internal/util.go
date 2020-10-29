package internal

import (
	"crypto/ecdsa"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
)

func ErrorExit(code int, f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, f, a...)
	os.Exit(code)
}

func ReadABI(fn string) (*abi.ABI, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := abi.JSON(f)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func ParseKey(b []byte, encrypted bool, password string) (*ecdsa.PrivateKey, error) {
	if encrypted {
		ksk, err := keystore.DecryptKey(b, password)
		if err != nil {
			return nil, err
		}
		return ksk.PrivateKey, nil
	}
	k, err := crypto.HexToECDSA(string(b))
	if err != nil {
		return nil, err
	}
	return k, nil
}
