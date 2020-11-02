package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/c-bata/go-prompt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/heliorosa/scui/internal"
	"github.com/heliorosa/scui/signer"
	"github.com/heliorosa/scui/ui"
)

var txSigner *signer.Signer

func main() {
	if len(os.Args) != 4 {
		internal.ErrorExit(-1, "missing arguments: usage: %s <client_url> <address> <abi_file>\n", os.Args[0])
	}
	// dial client
	cl, err := ethclient.Dial(os.Args[1])
	if err != nil {
		internal.ErrorExit(-2, "can't dial client: %s\n", err)
	}
	defer cl.Close()
	// parse contract address
	contractAddr := common.HexToAddress(os.Args[2])
	// read and parse abi file
	contractABI, err := internal.ReadABI(os.Args[3])
	if err != nil {
		internal.ErrorExit(-3, "can't read abi: %s\n", err)
	}
	// setup constant and transaction method calls
	constantNode, transactNode := methodsMenus(contractABI.Methods)
	// setup events
	eventsNode, listEventNode, watchEventNode := eventsMenu(contractABI.Events)
	// setup root node
	rootNode := ui.NewRootNode([]*ui.MenuCompleter{
		constantNode,
		transactNode,
		eventsNode,
		newSignerMenu(),
	})
	curNode := rootNode
	fmt.Printf("\nWelcome to scui.\nType \"help\" for a list of available commands or press <TAB> for auto-complete\n\n")
	for {
		inp := prompt.Input(curNode.Prompt(">"), curNode.Completer)
	Outer:
		switch inp {
		case "exit":
			os.Exit(0)
		case "help":
			ui.ShowHelp(curNode)
		case "..":
			if curNode != rootNode {
				curNode = curNode.Parent
			}
		case "":
		default:
			var sub *ui.MenuCompleter
			for _, i := range curNode.Sub {
				if i.Suggestion.Text == inp {
					sub = i
					break
				}
			}
			if sub == nil {
				fmt.Printf("invalid command: %s\n", inp)
				break Outer
			}
			if sub.Sub == nil {
				switch sub.Parent {
				case constantNode:
					r, err := executeConstantMethod(cl, &contractAddr, contractABI, sub.Suggestion.Text)
					if err != nil {
						fmt.Printf(
							"can't execute contant method \"%s\": %s\n",
							sub.Suggestion.Text,
							err,
						)
						break
					}
					fmt.Printf("returned:\n")
					for nj, j := range r {
						b, err := json.Marshal(j)
						if err != nil {
							fmt.Printf("can't marshal result: %s\n", err)
							break
						}
						me := contractABI.Methods[sub.Suggestion.Text].Outputs[nj]
						pref := me.Type.String()
						if me.Name != "" {
							pref += " " + me.Name
						}
						fmt.Printf("  (%s) %s\n", pref, string(b))
					}
				case transactNode:
					if txSigner.Kind() == signer.None {
						fmt.Printf("signer not set\n")
						break
					}
					tx, err := executeTransactMethod(cl, &contractAddr, contractABI, sub.Suggestion.Text)
					if err != nil {
						fmt.Printf(
							"can't send transaction to method %s: %s\n",
							sub.Suggestion.Text,
							err,
						)
						break
					}
					fmt.Printf("transaction sent: %s\n", tx.Hash().Hex())
				case listEventNode:
					listEvents(cl, &contractAddr, contractABI, sub.Suggestion.Text)
				case watchEventNode:
					watchEvents(cl, &contractAddr, contractABI, sub.Suggestion.Text)
				default:
					cmd := sub.Name()
					cmdFunc, ok := menuCommands[cmd]
					if ok {
						cmdFunc()
					} else {
						fmt.Printf("command not defined: %s\n", cmd)
					}
				}
			} else {
				curNode = sub
				break Outer
			}
		}
	}
}
