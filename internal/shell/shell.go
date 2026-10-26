// Package shell is gowec's interactive console: a banner, then a prompt that
// runs one command per line, with history and tab completion.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"
	"github.com/google/shlex"

	"github.com/WiLine-Edge-Cloud/gowec/internal/cli"
	"github.com/WiLine-Edge-Cloud/gowec/internal/config"
	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
)

// Run opens the console and returns the exit code when the person leaves.
func Run(app *cli.App) int {
	status, expired := cli.Status(app, "")
	fmt.Fprint(app.Out, ui.Banner(app.Version, status))

	_ = os.MkdirAll(config.Dir(), 0o700)
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:          prompt(app, expired),
		HistoryFile:     filepath.Join(config.Dir(), "history"),
		HistoryLimit:    1000,
		AutoComplete:    &completer{app: app},
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Fprintln(app.Err, ui.Fail(err.Error()))
		return cli.ExitFailed
	}
	defer rl.Close()

	app.InShell = true
	app.Prompter = prompter{rl}

	for {
		line, err := rl.ReadLine()
		if errors.Is(err, readline.ErrInterrupt) {
			continue
		}
		if err != nil { // io.EOF: Ctrl-D
			fmt.Fprintln(app.Out, ui.Dim.Render("bye"))
			return cli.ExitOK
		}
		line = strings.TrimSpace(line)
		switch line {
		case "":
			continue
		case "exit", "quit":
			fmt.Fprintln(app.Out, ui.Dim.Render("bye"))
			return cli.ExitOK
		case "clear":
			fmt.Fprint(app.Out, "\033[H\033[2J")
			continue
		}

		args, err := shlex.Split(line)
		if err != nil {
			fmt.Fprintln(app.Err, ui.Fail("can't read that line: "+err.Error()))
			continue
		}
		if args[0] == "gowec" { // typed out of habit
			args = args[1:]
		}
		runLine(app, args)

		// Logging in or switching changes who the prompt speaks for.
		if len(args) > 0 && (args[0] == "auth" || args[0] == "context" || args[0] == "ctx" || args[0] == "contexts") {
			_, expired = cli.Status(app, "")
			rl.SetPrompt(prompt(app, expired))
		}
	}
}

// runLine runs one command on a fresh command tree. Ctrl-C cancels it (and
// any wait) without leaving the console.
func runLine(app *cli.App, args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.Execute(ctx, app, args); err != nil {
		app.HandleError(err)
	}
}

func prompt(app *cli.App, expired bool) string {
	name := app.Cfg.Resolve("").Name
	if name == "" {
		name = "no context"
	}
	style := ui.Accent
	if expired {
		style = ui.Red
	}
	return ui.Bold.Render("gowec") + " " + style.Render(name) + ui.Dim.Render(" › ")
}

// prompter asks questions through the line editor, which owns stdin.
type prompter struct{ rl *readline.Instance }

func (p prompter) Line(text string) (string, error) {
	cfg := p.rl.GetConfig()
	cfg.Prompt = text
	cfg.AutoComplete = nil
	cfg.HistoryLimit = -1 // answers stay out of the history
	line, err := p.rl.ReadLineWithConfig(cfg)
	if errors.Is(err, readline.ErrInterrupt) || errors.Is(err, io.EOF) {
		return "", nil // treated as "no"
	}
	return strings.TrimSpace(line), err
}

func (p prompter) Secret(text string) (string, error) {
	cfg := p.rl.GeneratePasswordConfig()
	cfg.Prompt = text
	line, err := p.rl.ReadLineWithConfig(cfg)
	return strings.TrimSpace(line), err
}

// completer completes through cobra's own __complete command, so commands,
// flags, and machine names complete the same way they do in bash or zsh.
type completer struct{ app *cli.App }

func (c *completer) Do(line []rune, pos int) ([][]rune, int) {
	text := string(line[:pos])
	words, err := shlex.Split(text)
	if err != nil {
		return nil, 0
	}
	if len(words) > 0 && words[0] == "gowec" {
		words = words[1:]
	}
	partial := ""
	if len(words) > 0 && !strings.HasSuffix(text, " ") {
		partial, words = words[len(words)-1], words[:len(words)-1]
	}

	var out bytes.Buffer
	root := cli.NewRoot(&cli.App{Version: c.app.Version, Cfg: c.app.Cfg, Prompter: c.app.Prompter,
		InShell: true, Out: &out, Err: io.Discard})
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs(append(append([]string{"__complete"}, words...), partial))
	_ = root.Execute()

	var candidates [][]rune
	for _, l := range strings.Split(out.String(), "\n") {
		if l == "" || strings.HasPrefix(l, ":") {
			continue
		}
		candidate, _, _ := strings.Cut(l, "\t")
		if candidate == "completion" || candidate == "help" && len(words) > 0 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(partial)) {
			candidates = append(candidates, []rune(candidate[len(partial):]+" "))
		}
	}
	return candidates, len([]rune(partial))
}
