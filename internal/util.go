package internal

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"syscall"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/ssh/terminal"
)

func ErrorExit(code int, f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, f, a...)
	os.Exit(code)
}

func ReadABI(fn string) (*abi.ABI, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, WrapError("can't open file", err)
	}
	defer f.Close()
	r, err := abi.JSON(f)
	if err != nil {
		return nil, WrapError("can't parse abi", err)
	}
	return &r, nil
}

func ParseKey(b []byte, encrypted bool, password string) (*ecdsa.PrivateKey, error) {
	if encrypted {
		ksk, err := keystore.DecryptKey(b, password)
		if err != nil {
			return nil, WrapError("can't decrypt key", err)
		}
		return ksk.PrivateKey, nil
	}
	k, err := crypto.HexToECDSA(string(b))
	if err != nil {
		return nil, WrapError("can't import key", err)
	}
	return k, nil
}

func PromptPassword() (string, error) {
	bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", WrapError("can't read from terminal", err)
	}
	return string(bytePassword), nil
}

type wrappedError struct {
	m string
	e error
}

func WrapError(msg string, err error) error { return &wrappedError{m: msg, e: err} }
func (we *wrappedError) Error() string      { return fmt.Sprintf("%s: %s", we.m, we.e) }
func (we *wrappedError) Unwrap() error      { return we.e }
