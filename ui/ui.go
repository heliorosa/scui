package ui

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/c-bata/go-prompt"
	"github.com/heliorosa/scui/internal"
	"github.com/spf13/cobra"
)

var (
	UpCommand = &MenuCompleter{Suggestion: &prompt.Suggest{Text: "..",
		Description: "move to the parent menu",
	}}
	HelpCommand = &MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "help",
		Description: "show the help for the current menu",
	}}
	ExitCommand = &MenuCompleter{Suggestion: &prompt.Suggest{
		Text:        "exit",
		Description: "exit the interactive console",
	}}
	TailCommands = []*MenuCompleter{UpCommand, HelpCommand, ExitCommand}
)

type MenuCompleter struct {
	Suggestion *prompt.Suggest
	Sub        []*MenuCompleter
	Parent     *MenuCompleter
}

func NewRootNode(entries []*MenuCompleter) *MenuCompleter {
	r := &MenuCompleter{Sub: append(make([]*MenuCompleter, 0, len(entries)+3), entries...)}
	for _, i := range r.Sub {
		i.Parent = r
	}
	r.Sub = append(r.Sub, HelpCommand, ExitCommand)
	return r
}

func NewMenuCompleter(cmd *cobra.Command, parent *MenuCompleter) *MenuCompleter {
	name := strings.SplitN(cmd.Use, " ", 2)[0]
	cmds := cmd.Commands()
	r := &MenuCompleter{
		Suggestion: &prompt.Suggest{
			Text:        name,
			Description: cmd.Short,
		},
		Parent: parent,
		Sub:    make([]*MenuCompleter, 0, len(cmds)+3),
	}
	for _, i := range cmd.Commands() {
		r.Sub = append(r.Sub, NewMenuCompleter(i, r))
	}
	r.Sub = append(r.Sub, TailCommands...)
	return r
}

const nameSep = "/"

func (cc *MenuCompleter) Name() string {
	c := cc
	parts := make([]string, 0, 8)
	for {
		if c == nil {
			break
		}
		if c.Suggestion != nil {
			parts = append(parts, c.Suggestion.Text)
		}
		c = c.Parent
	}
	sz := len(parts)
	for i := 0; i < sz/2; i++ {
		parts[i], parts[sz-i-1] = parts[sz-i-1], parts[i]
	}
	return strings.Join(parts, nameSep)
}

func (cc *MenuCompleter) Prompt(p string) string {
	return cc.Name() + p + " "
}

func (cc *MenuCompleter) Completer(doc prompt.Document) []prompt.Suggest {
	r := make([]prompt.Suggest, 0, len(cc.Sub))
	for _, i := range cc.Sub {
		r = append(r, *i.Suggestion)
	}
	return prompt.FilterHasPrefix(r, doc.GetWordBeforeCursor(), false)
}

func InputMultiChoice(pr string, def string, choices []prompt.Suggest, helpFunc func(c []prompt.Suggest)) (string, bool) {
	choices = append(choices, *TailCommands[0].Suggestion, *TailCommands[1].Suggestion)
	for {
		input := prompt.Input(fmt.Sprintf(pr, def), func(doc prompt.Document) []prompt.Suggest {
			return prompt.FilterHasPrefix(choices, doc.GetWordBeforeCursor(), false)
		})
		switch ii := strings.TrimSpace(input); ii {
		case "":
			return def, true
		case "..":
			fmt.Println("aborted")
			return "", false
		case "help":
			helpFunc(choices[:len(choices)-2])
		default:
			for _, i := range choices[:len(choices)-2] {
				if input == i.Text {
					return input, true
				}
			}
			fmt.Printf("invalid choice: %s\n", input)
		}
	}
}

func InputMultiChoiceString(pr string, def string, choices []string, helpFunc func(c []prompt.Suggest)) (string, bool) {
	c := make([]prompt.Suggest, 0, len(choices))
	for _, i := range choices {
		c = append(c, prompt.Suggest{Text: i})
	}
	return InputMultiChoice(pr, def, c, helpFunc)
}

var yesNoMap = map[string]bool{"yes": true, "no": false}

func InputYesNo(pr string, def bool) (bool, bool) {
	choices := make([]prompt.Suggest, 0, 2)
	for _, i := range []string{"no", "yes"} {
		choices = append(choices, prompt.Suggest{Text: i})
	}
	var d string
	if def {
		d = choices[1].Text
	} else {
		d = choices[0].Text
	}
	r, ok := InputMultiChoice(pr, d, choices, func(_ []prompt.Suggest) {
		fmt.Printf("\nchoose yes or no\n")
	})
	if !ok {
		return false, false
	}
	return yesNoMap[r], true
}

var pathSep = string([]rune{filepath.Separator})

func InputPath(pr string, rootPath string, mustExist bool, pathToSuggestionFn func(path string, text string) (prompt.Suggest, bool)) (string, error) {
	for {
		input := prompt.Input(pr, func(doc prompt.Document) []prompt.Suggest {
			r := make([]prompt.Suggest, 0, 0)
			text := doc.TextBeforeCursor()
			var fullPath string
			if filepath.IsAbs(text) {
				fullPath = text
			} else {
				fullPath = filepath.Join(rootPath, text)
			}
			var dirName string
			if text == "" {
				dirName = rootPath
			} else if filepath.IsAbs(text) {
				dirName, _ = filepath.Split(text)
			} else {
				if strings.HasSuffix(text, pathSep) {
					dirName = fullPath
				} else {
					dirName, _ = filepath.Split(fullPath)
				}
			}
			err := listPath(dirName, func(path string) {
				sug, ok := pathToSuggestionFn(path, text)
				if !ok {
					return
				}
				r = append(r, sug)
			})
			if err != nil {
				return nil
			}
			return r
		})
		if strings.TrimSpace(input) == "" {
			return "", nil
		}
		var r string
		if filepath.IsAbs(input) {
			r = input
		} else {
			r = filepath.Join(rootPath, input)
		}
		if mustExist {
			info, err := os.Stat(r)
			if err != nil {
				return "", err
			}
			if info.IsDir() {
				return "", errors.New("not a file")
			}
		}
		return r, nil
	}
}

func listPath(rootPath string, entryFn func(string)) error {
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			e, ok := err.(*os.PathError)
			if !ok {
				return err
			}
			if e.Err != syscall.ENOENT && e.Err != syscall.EACCES {
				return err
			}
			return nil
		}
		if info.IsDir() {
			if filepath.Clean(rootPath) == filepath.Clean(path) {
				return nil
			}
		}
		entryFn(path)
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func trimPath(s, p string) string {
	return strings.TrimPrefix(strings.TrimPrefix(s, p), pathSep)
}

func NewAbsolutePathFilter(rootPath string) func(string, string) (prompt.Suggest, bool) {
	return func(path string, text string) (prompt.Suggest, bool) {
		if filepath.IsAbs(text) && strings.HasPrefix(path, text) {
			return prompt.Suggest{Text: path}, true
		} else {
			if strings.HasPrefix(filepath.Clean(text), "..") {
				_, pathName := filepath.Split(path)
				textDir, textName := filepath.Split(text)
				if strings.HasPrefix(pathName, textName) {
					return prompt.Suggest{Text: textDir + pathName}, true
				}
			} else {
				if relPath := trimPath(path, rootPath); strings.HasPrefix(relPath, text) {
					return prompt.Suggest{Text: relPath}, true
				}
			}
		}
		return prompt.Suggest{}, false
	}
}

func InputFilename(pr string, rootPath string, mustExit bool) (string, error) {
	return InputPath(pr, rootPath, mustExit, NewAbsolutePathFilter(rootPath))
}

func ShowHelp(node *MenuCompleter) {
	maxSz := 0
	for _, i := range node.Sub {
		if newSz := len(i.Suggestion.Text); newSz > maxSz {
			maxSz = newSz
		}
	}
	maxSz += 4
	for _, i := range node.Sub {
		fmt.Printf(
			"%s%s%s\n",
			i.Suggestion.Text,
			strings.Repeat(" ", maxSz-len(i.Suggestion.Text)),
			i.Suggestion.Description,
		)
	}
}

func InputPassword() (string, error) {
	fmt.Print("password: ")
	return internal.PromptPassword()
}

func InputText(pr string) string {
	return prompt.Input(
		fmt.Sprintf("%s", pr),
		func(prompt.Document) []prompt.Suggest { return nil },
	)
}

func InputBigInt(pr string) *big.Int {
	for {
		v := InputText(pr)
		if v == "" {
			continue
		}
		if r, ok := new(big.Int).SetString(v, 10); ok {
			return r
		}
	}
}

func InputBigIntWithDefault(pr string, d *big.Int) *big.Int {
	for {
		v := InputText(fmt.Sprintf(pr, d))
		if v == "" {
			return d
		}
		if r, ok := new(big.Int).SetString(v, 10); ok {
			return r
		}
	}
}

func InputIntWithDefault(pr string, def int) (int, bool) {
	for {
		v := InputText(fmt.Sprintf(pr, def))
		if v == "" {
			return def, true
		} else if v == ".." {
			fmt.Println("aborted")
			return 0, false
		}
		i, err := strconv.Atoi(v)
		if err != nil {
			fmt.Printf("%#v is not a number: %s\n", v, err)
			continue
		}
		return i, true
	}
}
