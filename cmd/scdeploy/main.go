package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/heliorosa/scui/internal"
	"github.com/heliorosa/scui/signer"
)

func showHelpAndExit(msg string) {
	if msg != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", msg)
	}
	fmt.Fprintf(os.Stderr, "usage: %s <client_url> <bytecode_file> <abi_file> <signer_flag(s)> [gas_flag(s)] [--] [constructor_arguments]\n\n", os.Args[0])
	signerArgs.newFlagSet("args", flag.ExitOnError).Usage()
	os.Exit(-1)
}

// arguments defaults
var signerArgs = &signatureArgsParser{
	derivationPath: "m/44'/60'/x'/0/0",
	gasPrice:       "0",
	value:          "0",
}

func main() {
	// split arguments
	if len(os.Args) < 4 {
		showHelpAndExit("")
	}
	args := os.Args[4:]
	var constructorArgs []string
	for i, a := range args {
		if a == "--" {
			constructorArgs = args[i+1:]
			args = args[:i]
			break
		}
	}
	fs := signerArgs.newFlagSet("args", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		internal.ErrorExit(-2, "invalid arguments: %s\n", err)
	}
	sigArgs, err := signerArgs.signatureArgs()
	if err != nil {
		internal.ErrorExit(-3, "can't parse arguments: %s\n", err)
	}
	// read bytecode
	bc, err := ioutil.ReadFile(os.Args[2])
	if err != nil {
		internal.ErrorExit(-4, "can't read bytecode: %s\n", err)
	}
	bytecode := make([]byte, len(bc)/2)
	if _, err = hex.Decode(bytecode, bc); err != nil {
		internal.ErrorExit(-5, "can't parse bytecode: %s\n", err)
	}
	// parse abi
	abi, err := internal.ReadABI(os.Args[3])
	if err != nil {
		internal.ErrorExit(-6, "can't parse ABI: %s\n", err)
	}
	if argsReq, argsProv := len(abi.Constructor.Inputs), len(constructorArgs); argsReq != argsProv {
		internal.ErrorExit(-7, "expecting %d arguments for the constructor, only %d provided\n", argsReq, argsProv)
	}
	cArgs := make([]interface{}, 0, len(constructorArgs))
	for i, a := range constructorArgs {
		b, err := json.Marshal(reflect.Zero(abi.Constructor.Inputs[i].Type.GetType()).Interface())
		if err != nil {
			internal.ErrorExit(
				-8,
				"can't marshal zero value for %s: %s\n",
				abi.Constructor.Inputs[i].Type.String(),
				err,
			)
		}
		if len(b) > 0 && b[0] == '"' {
			a = `"` + a + `"`
		}
		var rv reflect.Value
		ty := abi.Constructor.Inputs[i].Type.GetType()
		if ty.Kind() == reflect.Ptr {
			rv = reflect.New(ty.Elem())
		} else {
			rv = reflect.New(ty)
		}
		r := rv.Interface()
		if err = json.Unmarshal([]byte(a), r); err != nil {
			internal.ErrorExit(-9, "can't unmarshal constructor argument: %s\n", err)
		}
		if ty.Kind() != reflect.Ptr {
			cArgs = append(cArgs, rv.Elem().Interface())
		} else {
			cArgs = append(cArgs, r)
		}
	}
	// dial client
	cl, err := ethclient.Dial(os.Args[1])
	if err != nil {
		internal.ErrorExit(-10, "can't dial client: %s\n", err)
	}
	defer cl.Close()
	// deploy contract
	chainID, err := cl.ChainID(context.Background())
	if err != nil {
		internal.ErrorExit(-11, "can't get chain id: %s\n", err)
	}
	addr, tx, _, err := bind.DeployContract(sigArgs.transactOpts(chainID), *abi, bytecode, cl, cArgs...)
	if err != nil {
		internal.ErrorExit(-11, "can't deploy contract: %s\n", err)
	}
	fmt.Printf("contract deployed to address %s\ntxid: %s\n", addr.Hex(), tx.Hash().Hex())
}

type signatureArgsParser struct {
	rawKey         string
	encKey         string
	keyPassword    string
	ledger         bool
	derivationPath string
	ledgerAddr     string
	gasLimit       uint64
	gasPrice       string
	value          string
}

func (sap *signatureArgsParser) newFlagSet(name string, errorHandling flag.ErrorHandling) *flag.FlagSet {
	fs := flag.NewFlagSet(name, errorHandling)
	fs.StringVar(&sap.rawKey, "k", sap.rawKey, "raw key")
	fs.StringVar(&sap.encKey, "e", sap.encKey, "encrypted key")
	fs.StringVar(&sap.keyPassword, "P", sap.keyPassword, "password for the encrypted key")
	fs.StringVar(&sap.gasPrice, "p", sap.gasPrice, "gas price")
	fs.Uint64Var(&sap.gasLimit, "l", sap.gasLimit, "gas limit")
	fs.StringVar(&sap.value, "v", sap.value, "value to send")
	fs.BoolVar(&sap.ledger, "w", sap.ledger, "sign with ledger")
	fs.StringVar(&sap.derivationPath, "d", sap.derivationPath, "derivation path")
	fs.StringVar(&sap.ledgerAddr, "a", sap.ledgerAddr, "address (empty to use the first in the derivation path)")
	return fs
}

var (
	errInvalidGasPrice       = errors.New("invalid gas price")
	errInvalidAmount         = errors.New("invalid amount")
	errSignerMissing         = errors.New("signer arguments missing")
	errSignerAddressNotfound = errors.New("signer address not found")
	errLedgerNotFound        = errors.New("ledger not found")
)

type mutuallExclusiveArgsError [2]string

func (e mutuallExclusiveArgsError) Error() string {
	return fmt.Sprintf("%s and %s are mutually exclusive", e[0], e[1])
}

func (sap *signatureArgsParser) signatureArgs() (*signatureArgs, error) {
	r := &signatureArgs{}
	// parse gas price
	bigZero := big.NewInt(0)
	if sap.gasPrice != "" {
		gp, ok := new(big.Int).SetString(sap.gasPrice, 10)
		if !ok || gp.Cmp(bigZero) < 0 {
			return nil, errInvalidGasPrice
		} else if gp.Cmp(bigZero) > 0 {
			r.gasPrice = gp
		}
	}
	// parse gas limit
	if sap.gasLimit > 0 {
		r.gasLimit = sap.gasLimit
	}
	if !sap.ledger && sap.rawKey == "" && sap.encKey == "" {
		return nil, errSignerMissing
	}
	// parse amount to send
	if sap.value != "" {
		v, ok := new(big.Int).SetString(sap.value, 10)
		if !ok || v.Cmp(bigZero) < 0 {
			return nil, errInvalidAmount
		} else if v.Cmp(bigZero) > 0 {
			r.value = v
		}
	}
	// sign with a key
	if !sap.ledger {
		if sap.rawKey != "" && sap.encKey != "" {
			return nil, mutuallExclusiveArgsError{"-e", "-k"}
		}
		var (
			kb          []byte
			kf          string
			err         error
			isEncrypted bool
		)
		if sap.rawKey != "" {
			kf = sap.rawKey
		} else {
			isEncrypted = true
			kf = sap.encKey
			if sap.keyPassword == "" {
				if sap.keyPassword, err = internal.PromptPassword(); err != nil {
					return nil, err
				}
			}
		}
		if kb, err = ioutil.ReadFile(kf); err != nil {
			return nil, internal.WrapError("can't read file", err)
		}
		key, err := internal.ParseKey(kb, isEncrypted, sap.keyPassword)
		if err != nil {
			return nil, err
		}
		r.signer = signer.NewKeyed(key)
		return r, nil
	}
	// sign with ledger
	if sap.rawKey != "" {
		return nil, mutuallExclusiveArgsError{"-w", "-k"}
	}
	if sap.encKey != "" {
		return nil, mutuallExclusiveArgsError{"-w", "-e"}
	}
	if sap.keyPassword != "" {
		return nil, mutuallExclusiveArgsError{"-w", "-P"}
	}
	// new hub
	hub, err := usbwallet.NewLedgerHub()
	if err != nil {
		return nil, internal.WrapError("can't open ledger hub", err)
	}
	// pick wallet
	var w accounts.Wallet
	if sz := len(hub.Wallets()); sz == 0 {
		return nil, errLedgerNotFound
	} else if sz != 1 {
		return nil, internal.WrapError("more than one hardware wallet detected", nil)
	}
	w = hub.Wallets()[0]
	// open
	if err = w.Open(""); err != nil {
		return nil, internal.WrapError("can't open wallet", err)
	}
	dp := strings.ReplaceAll(sap.derivationPath, "x", "%d")
	if sap.ledgerAddr == "" {
		acc, err := deriveAddress(w, dp, 0)
		if err != nil {
			return nil, internal.WrapError("can't derive address", err)
		}
		r.signer = signer.NewLedger(w, acc)
		return r, nil
	}
	sigAddr := common.HexToAddress(sap.ledgerAddr)
	for i := 0; i < 5; i++ {
		acc, err := deriveAddress(w, dp, i)
		if err != nil {
			return nil, internal.WrapError("can't derive address", err)
		}
		if acc.Address == sigAddr {
			r.signer = signer.NewLedger(w, acc)
			return r, nil
		}
	}
	return nil, errSignerAddressNotfound
}

func deriveAddress(w accounts.Wallet, dp string, n int) (*accounts.Account, error) {
	adp, err := accounts.ParseDerivationPath(fmt.Sprintf(dp, n))
	if err != nil {
		return nil, internal.WrapError("can't parse derivation path", err)
	}
	acc, err := w.Derive(adp, true)
	if err != nil {
		return nil, internal.WrapError("can't derive address", err)
	}
	return &acc, nil
}

type signatureArgs struct {
	signer   *signer.Signer
	gasPrice *big.Int
	gasLimit uint64
	value    *big.Int
}

func (sa *signatureArgs) transactOpts(chainID *big.Int) *bind.TransactOpts {
	r, err := sa.signer.TransactOpts(chainID)
	if err != nil {
		panic(err)
	}
	r.GasLimit = sa.gasLimit
	r.GasPrice = sa.gasPrice
	r.Value = sa.value
	return r
}
