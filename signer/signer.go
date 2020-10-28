package signer

import (
	"crypto/ecdsa"
	"errors"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/external"
	"github.com/ethereum/go-ethereum/common"
)

type Signer struct {
	Key    *ecdsa.PrivateKey
	Wallet *walletSigner
}

type walletSigner struct {
	Wallet  accounts.Wallet
	Account accounts.Account
}

func NewKeyed(key *ecdsa.PrivateKey) *Signer { return &Signer{Key: key} }

func NewLedger(w accounts.Wallet, a common.Address) *Signer {
	return &Signer{
		Wallet: &walletSigner{
			Wallet:  w,
			Account: accounts.Account{Address: a},
		},
	}
}

type Kind int

const (
	None Kind = iota
	Keyed
	HardwareWallet
)

func (s *Signer) Kind() Kind {
	if s.Key != nil {
		return Keyed
	}
	if s.Wallet != nil {
		return HardwareWallet
	}
	return None
}

var ErrNoSigner = errors.New("signer not configured")

func (s *Signer) TransactOpts() (*bind.TransactOpts, error) {
	if sk := s.Kind(); sk == Keyed {
		return bind.NewKeyedTransactor(s.Key), nil
	} else if sk != HardwareWallet {
		return nil, ErrNoSigner
	}
	es, err := external.NewExternalSigner(s.Wallet.Wallet.URL().String())
	if err != nil {
		return nil, err
	}
	return bind.NewClefTransactor(es, s.Wallet.Account), nil
}
