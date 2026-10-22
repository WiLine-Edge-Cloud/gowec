// gowec is the WEC cloud's terminal client.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"git.wiline.com/cloud/cloud-cli/internal/cli"
	"git.wiline.com/cloud/cloud-cli/internal/config"
	"git.wiline.com/cloud/cloud-cli/internal/shell"
	"git.wiline.com/cloud/cloud-cli/internal/ui"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Fail(err.Error()))
		os.Exit(cli.ExitFailed)
	}
	app := &cli.App{Version: version, Cfg: cfg, Prompter: ui.TermPrompter{}, Out: os.Stdout, Err: os.Stderr}

	if len(os.Args) == 1 && ui.IsTerminal(os.Stdin) && ui.IsTerminal(os.Stdout) {
		os.Exit(shell.Run(app))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err = cli.Execute(ctx, app, os.Args[1:])
	stop()
	os.Exit(cli.HandleError(os.Stderr, err))
}
