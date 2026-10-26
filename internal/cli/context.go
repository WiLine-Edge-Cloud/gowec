package cli

import (
	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
)

func contextCmd(r *run) *cobra.Command {
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the saved contexts",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg := r.app.Cfg
			type view struct {
				Name    string `json:"name"`
				APIURL  string `json:"api_url"`
				Current bool   `json:"current"`
				HasKey  bool   `json:"has_key"`
			}
			views := []view{}
			for _, name := range cfg.Names() {
				ctx := cfg.Contexts[name]
				views = append(views, view{name, ctx.APIURL, name == cfg.CurrentContext, ctx.Token != ""})
			}
			ids := make([]string, len(views))
			rows := make([][]string, len(views))
			for i, v := range views {
				ids[i] = v.Name
				mark, key := " ", ui.Dim.Render("no key")
				if v.Current {
					mark = ui.Accent.Render("●")
				}
				if v.HasKey {
					key = "key saved"
				}
				rows[i] = []string{mark + " " + v.Name, v.APIURL, key}
			}
			return r.show(views, ids, func() string {
				if len(views) == 0 {
					return ui.Dim.Render(r.app.Refs("No contexts yet: run gowec auth login")) + "\n"
				}
				return ui.Table([]string{"  CONTEXT", "API", "KEY"}, rows)
			})
		},
	}
	use := &cobra.Command{
		Use:   "use <name>",
		Short: "Switch to another context",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return r.app.Cfg.Names(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			if r.app.Cfg.Contexts[args[0]] == nil {
				return usagef("no context %q: create it with gowec auth login --context %s", args[0], args[0])
			}
			r.app.Cfg.CurrentContext = args[0]
			if err := r.app.Cfg.Save(); err != nil {
				return err
			}
			r.app.ForgetNames()
			r.println(ui.Success("Now using " + ui.Accent.Render(args[0])))
			return nil
		},
	}
	return group("context", "Switch between API hosts and keys", []string{"contexts", "ctx"}, list, use)
}
