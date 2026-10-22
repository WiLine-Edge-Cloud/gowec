package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// Execute runs one command line. Errors cobra raises before a command's body
// runs (unknown commands or flags, wrong arguments, missing required flags)
// come back as usage errors.
func Execute(ctx context.Context, app *App, args []string) error {
	root := NewRoot(app)
	ran := false
	var wrap func(*cobra.Command)
	wrap = func(cmd *cobra.Command) {
		if body := cmd.RunE; body != nil {
			cmd.RunE = func(c *cobra.Command, a []string) error {
				ran = true
				return body(c, a)
			}
		}
		for _, child := range cmd.Commands() {
			wrap(child)
		}
	}
	wrap(root)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil && !ran {
		return &UsageError{err.Error()}
	}
	return err
}

// NewRoot builds the whole command tree for one command line.
func NewRoot(app *App) *cobra.Command {
	r := &run{app: app}
	root := &cobra.Command{
		Use:   "gowec",
		Short: "The WEC cloud from your terminal",
		Long: "Manage your WEC cloud with an API key. Run gowec alone for the\n" +
			"interactive console, or give it a command.",
		Example: "  gowec auth login\n" +
			"  gowec vm list\n" +
			"  gowec vm start web-1\n" +
			"  gowec vm list -o json",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetOut(app.Out)
	root.SetErr(app.Err)
	installHelp(app, root)
	root.CompletionOptions.HiddenDefaultCmd = app.InShell

	flags := root.PersistentFlags()
	flags.StringVar(&r.context, "context", "", "the context to use (see gowec context list)")
	flags.StringVarP(&r.output, "output", "o", "table", "output format: table, json or yaml")
	flags.BoolVarP(&r.detach, "detach", "d", false, "don't wait for background work; print its id")
	flags.BoolVarP(&r.yes, "yes", "y", false, "answer yes to confirmations")
	flags.BoolVarP(&r.quiet, "quiet", "q", false, "print ids only")
	_ = root.RegisterFlagCompletionFunc("output", fixed("table", "json", "yaml"))
	_ = root.RegisterFlagCompletionFunc("context", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return app.Cfg.Names(), cobra.ShellCompDirectiveNoFileComp
	})

	root.AddGroup(
		&cobra.Group{ID: "resources", Title: "Resources:"},
		&cobra.Group{ID: "follow", Title: "Following work:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)
	for _, cmd := range []*cobra.Command{
		machineCmd(r), volumeCmd(r), snapshotCmd(r), backupCmd(r),
		networkCmd(r), vpcCmd(r), addressCmd(r), firewallCmd(r), sshKeyCmd(r),
		planCmd(r), walletCmd(r), ticketCmd(r),
		zoneCmd(r), sizeCmd(r), templateCmd(r), diskCmd(r), priceCmd(r), bundleCmd(r),
	} {
		cmd.GroupID = "resources"
		root.AddCommand(cmd)
	}
	for _, cmd := range []*cobra.Command{jobCmd(r), launchCmd(r), upgradeCmd(r)} {
		cmd.GroupID = "follow"
		root.AddCommand(cmd)
	}
	for _, cmd := range []*cobra.Command{authCmd(r), contextCmd(r), versionCmd(r)} {
		cmd.GroupID = "setup"
		root.AddCommand(cmd)
	}
	root.SetCompletionCommandGroupID("setup")

	app.commands = map[string]bool{"help": true}
	for _, cmd := range root.Commands() {
		app.commands[cmd.Name()] = true
		for _, alias := range cmd.Aliases {
			app.commands[alias] = true
		}
	}
	return root
}

func fixed(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

// group builds a resource command whose actions are its subcommands.
func group(use, short string, aliases []string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Aliases: aliases, Args: cobra.NoArgs}
	cmd.AddCommand(children...)
	return cmd
}

func versionCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print gowec's version",
		Args:  cobra.NoArgs,
		Run: func(*cobra.Command, []string) {
			r.println("gowec " + r.app.Version)
		},
	}
}
