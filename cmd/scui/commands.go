package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/c-bata/go-prompt"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/usbwallet"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/transmutate-io/scui/signer"
	"github.com/transmutate-io/scui/ui"
)

var menuCommands = map[string]func(){
	"signer/key":    cmdConfigSignerKey,
	"signer/ledger": cmdConfigSignerLedger,
	"signer/show":   cmdConfigSignerShow,
}

func cmdConfigSignerKey() {
	key, err := inputKeyFile()
	if err != nil {
		fmt.Printf("can't read key file: %s\n", err)
		return
	}
	txSigner = signer.NewKeyed(key)
}

func cmdConfigSignerLedger() {
	// close any previously open wallet
	if txSigner != nil &&
		txSigner.Kind() == signer.HardwareWallet &&
		txSigner.Wallet != nil {
		txSigner.Wallet.Wallet.Close()
	}
	// new hub
	hub, err := usbwallet.NewLedgerHub()
	if err != nil {
		fmt.Printf("can't start hub: %s\n", err)
		return
	}
	// pick wallet
	var w accounts.Wallet
	if sz := len(hub.Wallets()); sz == 0 {
		fmt.Printf("can't find any wallets\n")
		return
	} else if sz == 1 {
		w = hub.Wallets()[0]
	} else {
		fmt.Printf("found more than one wallet. choose one.\n")
		wallets := hub.Wallets()
		wm := make(map[string]accounts.Wallet, len(wallets))
		wu := make([]string, 0, len(wallets))
		for _, i := range wallets {
			wm[i.URL().String()] = i
			wu = append(wu, i.URL().String())
		}
		u, ok := ui.InputMultiChoiceString("wallet (%s): ", wu[0], wu, func(c []prompt.Suggest) {
			fmt.Printf("choose one of the url's\n")
		})
		if !ok {
			fmt.Printf("aborted\n")
			return
		}
		w = wm[u]
	}
	// open
	if err = w.Open(""); err != nil {
		fmt.Printf("can't open wallet: %s\n", err)
		return
	}
	// check status
	st, err := w.Status()
	if err != nil {
		fmt.Printf("can't get status: %s\n", err)
	}
	fmt.Printf("wallet status: %s\n", st)
	dpTemplates := []string{"m/44'/60'/x'/0/0", "m/44'/60'/0'/x", "custom"}
	dp, ok := ui.InputMultiChoiceString("derivation path (%s): ", dpTemplates[0], dpTemplates, func(c []prompt.Suggest) {
		fmt.Printf("choose one of the derivation paths or custom\n")
	})
	if !ok {
		fmt.Printf("aborted\n")
		return
	}
	if dp == "custom" {
		dp = ui.InputText("custom derivation path: ")
	}
	dp = strings.Replace(dp, "x", "%d", -1)
	// pick address
	addrIdx := 0
	addrs := make([]string, 0, 5)
	for {
		for i := 0; i < 5; i++ {
			dp, err := accounts.ParseDerivationPath(fmt.Sprintf(dp, addrIdx))
			if err != nil {
				fmt.Printf("can't parse derivation path: %s\n", err)
				return
			}
			addrIdx++
			acc, err := w.Derive(dp, false)
			if err != nil {
				fmt.Printf("can't derive address: %s\n", err)
				return
			}
			addrs = append(addrs, acc.Address.Hex())
		}
		choices := append(make([]string, 0, len(addrs)+1), addrs...)
		choices = append(choices, "more")
		a, ok := ui.InputMultiChoiceString("address (%s): ", addrs[0], choices, func(c []prompt.Suggest) {
			fmt.Printf("pick one address or more to generate\n")
		})
		if !ok {
			fmt.Printf("aborted\n")
			return
		}
		if a == "more" {
			continue
		}
		txSigner = signer.NewLedger(w, common.HexToAddress(a))
		break
	}
}

func cmdConfigSignerShow() {
	if sk := txSigner.Kind(); sk == signer.None {
		fmt.Printf("no signer set\n")
		return
	} else if sk == signer.Keyed {
		fmt.Printf(
			"sign with a key.\naddress: %s\n",
			crypto.PubkeyToAddress(txSigner.Key.PublicKey).Hex(),
		)
		return
	}
	st, err := txSigner.Wallet.Wallet.Status()
	if err != nil {
		fmt.Printf("can't get hardware wallet status: %s\n", err)
		return
	}
	fmt.Printf(
		"sign with hardware wallet.\n%s\naddress: %s\n",
		st,
		txSigner.Wallet.Account.Address.Hex(),
	)
}

var (
	errNotConstant = errors.New("method is not constant")
	errConstant    = errors.New("method is constant")
	errAborted     = errors.New("aborted")
)

func executeConstantMethod(cl *ethclient.Client, addr *common.Address, abi *abi.ABI, name string) ([]interface{}, error) {
	fmt.Printf("constant call arguments:\n")
	args, err := inputArguments(abi.Methods[name].Inputs, false)
	if err != nil {
		return nil, err
	}
	bc := bind.NewBoundContract(*addr, *abi, cl, cl, cl)
	method := abi.Methods[name]
	if !method.IsConstant() {
		return nil, errNotConstant
	}
	// call method
	res := newCallResult(method.Outputs)
	if err := bc.Call(nil, res.res, name, args...); err != nil {
		return nil, err
	}
	return res.results(), nil
}

type callResult struct {
	mo  abi.Arguments
	res interface{}
}

func newCallResult(mo abi.Arguments) *callResult {
	switch len(mo) {
	case 0:
		return nil
	case 1:
		return &callResult{mo: mo, res: reflect.New(mo[0].Type.GetType()).Interface()}
	}
	r := make([]interface{}, 0, len(mo))
	for _, i := range mo {
		r = append(r, reflect.New(i.Type.GetType()).Interface())
	}
	return &callResult{mo: mo, res: &r}
}

func indirectInterface(v interface{}) interface{} {
	return reflect.Indirect(reflect.ValueOf(v)).Interface()
}

func (cr *callResult) results() []interface{} {
	if s, ok := cr.res.(*[]interface{}); ok {
		r := make([]interface{}, 0, 4)
		for _, i := range *s {
			r = append(r, indirectInterface(i))
		}
		return r
	}
	return []interface{}{indirectInterface(cr.res)}
}

func executeTransactMethod(cl *ethclient.Client, addr *common.Address, abi *abi.ABI, name string) (*types.Transaction, error) {
	method := abi.Methods[name]
	if method.IsConstant() {
		return nil, errConstant
	}
	fmt.Printf("transaction arguments:\n")
	args, err := inputArguments(abi.Methods[name].Inputs, false)
	if err != nil {
		return nil, err
	}
	bc := bind.NewBoundContract(*addr, *abi, cl, cl, cl)
	opts, err := txSigner.TransactOpts()
	if err != nil {
		return nil, err
	}
	if abi.Methods[name].IsPayable() {
		send, ok := ui.InputYesNo("method is payable. send amount with transaction? (%s): ", false)
		if !ok {
			return nil, errAborted
		}
		if send {
			amount := ui.InputBigInt("amount: ")
			opts.Value = amount
		}
	}
	if estimateGasPrice, ok := ui.InputYesNo("estimate gas price? (%s): ", true); !ok {
		return nil, errAborted
	} else if !estimateGasPrice {
		sugg, err := cl.SuggestGasPrice(context.Background())
		if err != nil {
			return nil, err
		}
		opts.GasPrice = ui.InputBigIntWithDefault("gas price (%s): ", sugg)
	}
	if estimateGasLimit, ok := ui.InputYesNo("estimate gas limit? (%s): ", true); !ok {
		return nil, errAborted
	} else if !estimateGasLimit {
		if gl, ok := ui.InputIntWithDefault("gas limit (%d): ", 0); ok {
			opts.GasLimit = uint64(gl)
		}
	}
	return bc.Transact(opts, name, args...)
}

func listEvents(cl *ethclient.Client, addr *common.Address, abi *abi.ABI, name string) {
	filters, err := inputFilters(abi.Events[name].Inputs)
	if err != nil {
		fmt.Printf("error parsing filter fields: %s\n", err)
		return
	}
	opts := &bind.FilterOpts{}
	startBlock, ok := ui.InputIntWithDefault("start block (%d): ", 0)
	if !ok {
		fmt.Printf("aborted\n")
		return
	}
	opts.Start = uint64(startBlock)
	if lastBlock, ok := ui.InputIntWithDefault("end block (last, %d): ", -1); !ok {
		fmt.Printf("aborted\n")
		return
	} else if lastBlock >= 0 {
		lb := uint64(lastBlock)
		opts.End = &lb
	}
	bc := bind.NewBoundContract(*addr, *abi, cl, cl, cl)
	logs, sub, err := bc.FilterLogs(opts, name, filters...)
	if err != nil {
		fmt.Printf("error listing logs: %s\n", err)
		return
	}
	defer close(logs)
	defer sub.Unsubscribe()
	for {
		if err := <-sub.Err(); err != nil {
			fmt.Printf("error listing logs: %s\n", err)
			return
		}
		select {
		case l := <-logs:
			eventData := make(map[string]interface{}, 8)
			if err := bc.UnpackLogIntoMap(eventData, name, l); err != nil {
				fmt.Printf("error listing logs: %s\n", err)
				return
			}
			fmt.Print(formatEvent(abi.Events[name].Inputs, eventData, l.BlockNumber))
		default:
			return
		}
	}

}

func formatEvent(inputs abi.Arguments, eventData map[string]interface{}, blockNumber uint64) string {
	var values []string
	for _, i := range inputs {
		b, err := json.Marshal(eventData[i.Name])
		if err != nil {
			fmt.Printf("can't marshal value: %s\n", err)
			continue
		}
		values = append(values, fmt.Sprintf("%s=%s", i.Name, string(b)))
	}
	return fmt.Sprintf("  block %d: %s\n", blockNumber, strings.Join(values, " "))
}

func watchEvents(cl *ethclient.Client, addr *common.Address, abi *abi.ABI, name string) {
	filters, err := inputFilters(abi.Events[name].Inputs)
	if err != nil {
		fmt.Printf("error parsing filter fields: %s\n", err)
		return
	}
	bc := bind.NewBoundContract(*addr, *abi, cl, cl, cl)
	logs, sub, err := bc.WatchLogs(nil, name, filters...)
	if err != nil {
		fmt.Printf("error watching logs: %s\n", err)
		return
	}
	defer close(logs)
	defer sub.Unsubscribe()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	for {
		select {
		case <-sig:
			return
		case err := <-sub.Err():
			if err != nil {
				fmt.Printf("error watching logs: %s\n", err)
				return
			}
		case l := <-logs:
			eventData := make(map[string]interface{}, 8)
			if err := bc.UnpackLogIntoMap(eventData, name, l); err != nil {
				fmt.Printf("error watching logs: %s\n", err)
				return
			}
			fmt.Print(formatEvent(abi.Events[name].Inputs, eventData, l.BlockNumber))
		}
	}
}
