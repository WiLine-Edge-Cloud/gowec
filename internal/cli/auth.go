package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/config"
	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func authCmd(r *run) *cobra.Command {
	return group("auth", "Log in with an API key", nil, authLoginCmd(r), authStatusCmd(r), authLogoutCmd(r))
}

func authLoginCmd(r *run) *cobra.Command {
	var apiURL string
	var fromStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save an API key under a context",
		Long: "Save an API key under a context. The key is checked against the API first.\n\n" +
			"Create a key in the portal: Settings → API access. It is shown once.\n" +
			"For scripts, pipe it in with --token-stdin, or skip the config and set\n" +
			"GOWEC_TOKEN (and GOWEC_API_URL) instead.",
		Example: "  gowec auth login\n" +
			"  gowec auth login --context local --api-url http://localhost:5050\n" +
			"  echo \"$WEC_KEY\" | gowec auth login --token-stdin",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name := r.context
			if name == "" {
				name = r.app.Cfg.CurrentContext
			}
			if name == "" {
				name = "default"
			}
			existing := r.app.Cfg.Contexts[name]
			if apiURL == "" && existing != nil {
				apiURL = existing.APIURL
			}
			if apiURL == "" {
				apiURL = config.DefaultAPIURL
			}

			token, err := readToken(r, fromStdin)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(token, "wec_") {
				return usagef("that doesn't look like a WEC API key (they start with wec_)")
			}

			c := wec.New(apiURL, token, "gowec/"+r.app.Version)
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Checking the key…")
			me, err := c.WhoAmI(cmd.Context())
			spin.Stop()
			if err != nil {
				return err
			}

			r.app.Cfg.Contexts[name] = &config.Context{APIURL: apiURL, Token: token}
			r.app.Cfg.CurrentContext = name
			if err := r.app.Cfg.Save(); err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.show(me, []string{me.KeyID}, func() string {
				return ui.Success("Logged in to "+ui.Bold.Render(dash(me.AccountName))+" as context "+ui.Accent.Render(name)) +
					"\n\n" + ui.Pairs(whoamiPairs(me, apiURL))
			})
		},
	}
	cmd.Flags().StringVar(&apiURL, "api-url", "", "the API host (default "+config.DefaultAPIURL+")")
	cmd.Flags().BoolVar(&fromStdin, "token-stdin", false, "read the key from stdin")
	return cmd
}

func readToken(r *run, fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return "", usagef("no key on stdin")
		}
		return strings.TrimSpace(line), nil
	}
	token, err := r.app.Prompter.Secret(ui.Accent.Render("?") + " API key " + ui.Dim.Render("(hidden)") + ": ")
	if err == ui.ErrNotInteractive {
		return "", usagef("no terminal to ask on: pass the key with --token-stdin")
	}
	return token, err
}

func authStatusCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show the key in use: its account, permissions and expiry",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			me, err := c.WhoAmI(cmd.Context())
			if err != nil {
				return err
			}
			return r.show(me, []string{me.KeyID}, func() string {
				pairs := [][2]string{{"Context", ui.Accent.Render(r.active().Name)}}
				return ui.Pairs(append(pairs, whoamiPairs(me, r.active().APIURL)...))
			})
		},
	}
}

func authLogoutCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget a context's key (the key stays valid)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			name := r.active().Name
			ctx := r.app.Cfg.Contexts[name]
			if ctx == nil || ctx.Token == "" {
				return usagef("context %q has no key saved", name)
			}
			ctx.Token = ""
			if err := r.app.Cfg.Save(); err != nil {
				return err
			}
			r.app.ForgetNames()
			r.println(ui.Success("Forgot the key for " + name + ". Revoke it in Settings → API access if it leaked."))
			return nil
		},
	}
}

func whoamiPairs(me *wec.WhoAmI, apiURL string) [][2]string {
	var granted []string
	for resource, level := range me.Permissions {
		if level != "none" {
			granted = append(granted, resource+" "+ui.Dim.Render(level))
		}
	}
	sort.Strings(granted)
	guards := []string{}
	if me.CanSpend {
		guards = append(guards, "can spend")
	}
	if me.CanDelete {
		guards = append(guards, "can delete")
	}
	if len(guards) == 0 {
		guards = append(guards, ui.Dim.Render("can't spend or delete"))
	}
	return [][2]string{
		{"Account", ui.Bold.Render(dash(me.AccountName))},
		{"Key", fmt.Sprintf("%s  %s", me.KeyName, ui.Dim.Render("··"+me.Last4+" · "+me.Env))},
		{"Acts as", dash(me.ActsAs)},
		{"Expires", Expiry(me.ExpiresAt)},
		{"Permissions", strings.Join(granted, ", ")},
		{"Guards", strings.Join(guards, ", ")},
		{"API", ui.Dim.Render(apiURL)},
	}
}

// Expiry says when a key expires, in red within a week.
func Expiry(at time.Time) string {
	left := time.Until(at)
	days := int(left.Hours() / 24)
	switch {
	case left <= 0:
		return ui.Red.Render("expired")
	case days < 1:
		return ui.Red.Render(fmt.Sprintf("in %d hours", int(left.Hours())))
	case days <= 7:
		return ui.Red.Render(fmt.Sprintf("in %d days", days))
	}
	return fmt.Sprintf("in %d days %s", days, ui.Dim.Render(at.Local().Format("(Jan 2, 2006)")))
}

// Status is the shell's one-line description of the active context.
func Status(app *App, flag string) (line string, expired bool) {
	active := app.Cfg.Resolve(flag)
	if active.Token == "" {
		return ui.Yellow.Render("●") + " not logged in " + ui.Dim.Render("— run auth login"), false
	}
	c := wec.New(active.APIURL, active.Token, "gowec/"+app.Version)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	me, err := c.WhoAmI(ctx)
	if err != nil {
		reason := err.Error()
		if apiErr, ok := err.(*wec.APIError); ok {
			reason = apiErr.Message
		}
		return ui.Red.Render("●") + " " + ui.Accent.Render(active.Name) + " " + ui.Dim.Render("· "+reason), true
	}
	return ui.Green.Render("●") + " " + ui.Accent.Render(active.Name) + ui.Dim.Render(" · ") +
		ui.Bold.Render(dash(me.AccountName)) + ui.Dim.Render(" · key ") + fmt.Sprintf("%q", me.KeyName) +
		ui.Dim.Render(" ··"+me.Last4+" · expires ") + Expiry(me.ExpiresAt), me.ExpiresAt.Before(time.Now())
}
