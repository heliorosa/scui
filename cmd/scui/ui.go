package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/c-bata/go-prompt"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/transmutate-io/scui/ui"
)

func newSignerMenu() *ui.MenuCompleter {
	r := &ui.MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "signer",
		Description: "configure signer",
	}}
	sigKey := &ui.MenuCompleter{Parent: r, Suggestion: &prompt.Suggest{
		Text:        "key",
		Description: "sign with a key",
	}}
	sigLedger := &ui.MenuCompleter{Parent: r, Suggestion: &prompt.Suggest{
		Text:        "ledger",
		Description: "sign with ledger",
	}}
	sigShow := &ui.MenuCompleter{Parent: r, Suggestion: &prompt.Suggest{
		Text:        "show",
		Description: "show signed configuration",
	}}
	r.Sub = append([]*ui.MenuCompleter{sigKey, sigLedger, sigShow}, ui.TailCommands...)
	return r
}

func inputKeyFile() (*ecdsa.PrivateKey, error) {
	p, err := filepath.Abs(".")
	if err != nil {
		return nil, err
	}
	keyFile, err := ui.InputFilename("key file: ", p, true)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	encrypted, ok := ui.InputYesNo("encrypted? (%s): ", false)
	if !ok {
		return nil, errors.New("bad response")
	}
	var password string
	if encrypted {
		if password, err = ui.InputPassword(); err != nil {
			return nil, err
		}
	}
	return parseKey(b, encrypted, password)
}

func methodsMenus(methods map[string]abi.Method) (*ui.MenuCompleter, *ui.MenuCompleter) {
	constantNode := &ui.MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "constant",
		Description: "make a call to a constant method",
	}}
	transactNode := &ui.MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "transact",
		Description: "make a transaction to a method",
	}}
	names := make([]string, 0, len(methods))
	for i := range methods {
		names = append(names, i)
	}
	sort.Strings(names)
	for _, name := range names {
		m := methods[name]
		n := &ui.MenuCompleter{
			Suggestion: &prompt.Suggest{
				Text:        name,
				Description: m.String(),
			},
		}
		if m.IsConstant() {
			n.Parent = constantNode
			constantNode.Sub = append(constantNode.Sub, n)
		} else {
			n.Parent = transactNode
			transactNode.Sub = append(transactNode.Sub, n)
		}
	}
	constantNode.Sub = append(constantNode.Sub, ui.TailCommands...)
	transactNode.Sub = append(transactNode.Sub, ui.TailCommands...)
	return constantNode, transactNode
}

func eventsMenu(events map[string]abi.Event) (*ui.MenuCompleter, *ui.MenuCompleter, *ui.MenuCompleter) {
	eventsNode := &ui.MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "events",
		Description: "filter/watch events",
	}}
	listNode := &ui.MenuCompleter{Parent: eventsNode, Suggestion: &prompt.Suggest{
		Text:        "list",
		Description: "list event",
	}}
	watchNode := &ui.MenuCompleter{Parent: eventsNode, Suggestion: &prompt.Suggest{
		Text:        "watch",
		Description: "watch event",
	}}
	eventsNode.Sub = append([]*ui.MenuCompleter{listNode, watchNode}, ui.TailCommands...)
	eventsNames := make([]string, 0, len(events))
	for i := range events {
		eventsNames = append(eventsNames, i)
	}
	sort.Strings(eventsNames)
	for _, name := range eventsNames {
		sug := &prompt.Suggest{Text: name, Description: events[name].String()}
		listNode.Sub = append(listNode.Sub, &ui.MenuCompleter{Suggestion: sug, Parent: listNode})
		watchNode.Sub = append(watchNode.Sub, &ui.MenuCompleter{Suggestion: sug, Parent: watchNode})
	}
	listNode.Sub = append(listNode.Sub, ui.TailCommands...)
	watchNode.Sub = append(watchNode.Sub, ui.TailCommands...)
	return eventsNode, listNode, watchNode
}

func inputArguments(args abi.Arguments, isFilter bool) ([]interface{}, error) {
	r := make([]interface{}, 0, len(args))
	for _, i := range args {
		for {
			val := ui.InputText(i.Name + " (" + i.Type.String() + "): ")
			if val == "" {
				fmt.Printf("....\n")
				continue
			}
			v, err := unmarshalValue(val, i.Type.GetType())
			if err != nil {
				return nil, err
			}
			r = append(r, v)
			break
		}
	}
	return r, nil
}

func unmarshalValue(val string, t reflect.Type) (interface{}, error) {
	if t.Elem().Kind() == reflect.Ptr {
		t = t.Elem()
	}
	v := reflect.New(t).Interface()
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > 1 && b[0] == '"' {
		val = "\"" + val + "\""
	}
	if err = json.Unmarshal([]byte(val), v); err != nil {
		return nil, err
	}
	return v, nil
}

func inputFilters(inputs abi.Arguments) ([][]interface{}, error) {
	r := make([][]interface{}, 0, 4)
	for _, i := range inputs {
		if !i.Indexed {
			continue
		}
		pr := fmt.Sprintf("field %s (%s) is indexed. filter? (%%s): ", i.Name, i.Type.String())
		if filterField, ok := ui.InputYesNo(pr, false); !ok {
			return nil, errAborted
		} else if filterField {
			for {
				v := ui.InputText("field value (none): ")
				if v == "" {
					r = append(r, nil)
					break
				}
				fv, err := unmarshalValue(v, i.Type.GetType())
				if err != nil {
					fmt.Printf("can't parse value: %s\n", err)
					continue
				}
				r = append(r, []interface{}{fv})
				break
			}
		}
	}
	return r, nil
}
