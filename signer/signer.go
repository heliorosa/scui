package signer

import (
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Signer struct {
	Key    *ecdsa.PrivateKey
	Wallet *walletSigner
}

type walletSigner struct {
	Wallet  accounts.Wallet
	Account *accounts.Account
}

func NewKeyed(key *ecdsa.PrivateKey) *Signer { return &Signer{Key: key} }

func NewLedger(w accounts.Wallet, ac *accounts.Account) *Signer {
	return &Signer{
		Wallet: &walletSigner{
			Wallet:  w,
			Account: ac,
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

var (
	ErrNoSigner        = errors.New("signer not configured")
	ErrAddressNotFound = errors.New("address not found")
)

func (s *Signer) TransactOpts(chainID *big.Int) (*bind.TransactOpts, error) {
	switch sk := s.Kind(); sk {
	case None:
		return nil, ErrNoSigner
	case Keyed:
		return bind.NewKeyedTransactor(s.Key), nil
	case HardwareWallet:
	}
	return &bind.TransactOpts{
		From: s.Wallet.Account.Address,
		Signer: func(signer types.Signer, fromAddr common.Address, tx *types.Transaction) (*types.Transaction, error) {
			if s.Wallet.Account.Address != fromAddr {
				return nil, ErrAddressNotFound
			}
			return s.Wallet.Wallet.SignTx(*s.Wallet.Account, tx, chainID)
		},
	}, nil
}
