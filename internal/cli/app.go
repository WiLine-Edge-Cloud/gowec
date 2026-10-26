// Package cli is gowec's command tree. NewRoot builds a fresh tree for every
// command, so the shell can run one line after another without flag values
// leaking between them.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/WiLine-Edge-Cloud/gowec/internal/config"
	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

// App is what lives across commands: the config, how to ask questions, and
// small caches the shell's completion uses.
type App struct {
	Version  string
	Cfg      *config.Config
	Prompter ui.Prompter
	InShell  bool
	Out      io.Writer
	Err      io.Writer

	mu    sync.Mutex
	names map[string]cachedNames
	// commands are the top-level command names and aliases, for Refs.
	commands map[string]bool
}

// commandRef is "gowec <command>" in help and hints, with any styling
// between the two words; a leading "| " marks a shell pipeline.
var commandRef = regexp.MustCompile(`(\| )?gowec((?:\x1b\[[0-9;]*m)*) ([a-z][\w-]*|<|-)`)

// Refs drops "gowec " before a command inside the console, where commands
// are typed without it. Text about gowec itself ("gowec quotes the price")
// and pipelines ("echo … | gowec auth login") keep it.
func (a *App) Refs(text string) string {
	if !a.InShell {
		return text
	}
	return commandRef.ReplaceAllStringFunc(text, func(match string) string {
		m := commandRef.FindStringSubmatch(match)
		if m[1] != "" || !(m[3] == "<" || m[3] == "-" || a.commands[m[3]]) {
			return match
		}
		return m[2] + m[3]
	})
}

// HandleError is the console's HandleError: its hints name commands as they
// are typed there.
func (a *App) HandleError(err error) int {
	var b strings.Builder
	code := HandleError(&b, err)
	fmt.Fprint(a.Err, a.Refs(b.String()))
	return code
}

type cachedNames struct {
	names []string
	at    time.Time
}

// run is one command's flags and helpers.
type run struct {
	app     *App
	context string
	output  string
	detach  bool
	yes     bool
	quiet   bool
}

// UsageError is a mistake in how a command was called (exit code 2).
type UsageError struct{ msg string }

func (e *UsageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &UsageError{fmt.Sprintf(format, a...)} }

// errAborted is a "no" at a confirmation.
var errAborted = errors.New("cancelled, nothing was changed")

func (r *run) active() config.Active { return r.app.Cfg.Resolve(r.context) }

func (r *run) client() (*wec.Client, error) {
	active := r.active()
	if active.Token == "" {
		return nil, usagef("not logged in: run gowec auth login")
	}
	return wec.New(active.APIURL, active.Token, "gowec/"+r.app.Version), nil
}

// tty reports whether progress may be drawn: a terminal and a table output.
func (r *run) tty() bool { return r.output == "table" && !r.quiet && ui.IsTerminal(os.Stderr) }

func (r *run) println(a ...any) { fmt.Fprintln(r.app.Out, a...) }

// show prints v as asked: JSON or YAML as the API sent it, ids only with -q,
// or the table the command built.
func (r *run) show(v any, ids []string, table func() string) error {
	if r.quiet {
		for _, id := range ids {
			r.println(id)
		}
		return nil
	}
	switch r.output {
	case "json":
		enc := json.NewEncoder(r.app.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case "yaml":
		// Through JSON, so YAML keys are the API's field names.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var plain any
		if err := json.Unmarshal(data, &plain); err != nil {
			return err
		}
		out, err := yaml.Marshal(plain)
		if err != nil {
			return err
		}
		_, err = r.app.Out.Write(out)
		return err
	case "table":
		fmt.Fprint(r.app.Out, table())
		return nil
	}
	return usagef("unknown output %q: use table, json or yaml", r.output)
}

// confirm asks before something that spends or destroys. --yes answers for
// the person; without a terminal the command refuses rather than guess.
func (r *run) confirm(question, needs string) error {
	if r.yes {
		return nil
	}
	ok, err := ui.Confirm(r.app.Prompter, question)
	if errors.Is(err, ui.ErrNotInteractive) {
		return usagef("%s: confirm with --yes", needs)
	}
	if err != nil {
		return err
	}
	if !ok {
		return errAborted
	}
	return nil
}

// waitJob follows a CloudStack job to its end, unless --detach.
func (r *run) waitJob(ctx context.Context, c *wec.Client, accepted *wec.JobAccepted, doing, done string) error {
	if r.detach {
		return r.show(accepted, []string{accepted.JobID}, func() string {
			return ui.Success(doing+" · job "+accepted.JobID) + "\n" +
				ui.Dim.Render(r.app.Refs("  follow it with: gowec job wait "+accepted.JobID)) + "\n"
		})
	}
	spin := ui.StartSpinner(r.app.Err, r.tty(), doing+"…")
	job, err := c.WaitJob(ctx, accepted.JobID, nil)
	spin.Stop()
	if err != nil {
		return err
	}
	if job.Status == "failed" {
		code := ""
		if job.ErrorCode != nil {
			code = fmt.Sprintf(" (CloudStack error %d)", *job.ErrorCode)
		}
		return fmt.Errorf("%s failed%s; the account's events have the details · job %s", doing, code, job.JobID)
	}
	return r.show(job, []string{job.ResourceID}, func() string { return ui.Success(done) + "\n" })
}

func (r *run) idempotencyKey() string { return "gowec-" + uuid.NewString() }

// NameLister lists one kind of resource's names, for tab completion.
type NameLister func(ctx context.Context, c *wec.Client) ([]string, error)

// completeNames is a ValidArgsFunction completing the first argument with the
// names list returns. Names are kept 30s per context and kind, so each Tab
// doesn't call the API.
func (r *run) completeNames(kind string, list NameLister) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return r.app.cachedNames(r, kind, list), cobra.ShellCompDirectiveNoFileComp
	}
}

func (a *App) cachedNames(r *run, kind string, list NameLister) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := r.active().Name + "/" + kind
	if hit, ok := a.names[key]; ok && time.Since(hit.at) < 30*time.Second {
		return hit.names
	}
	c, err := r.client()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	names, err := list(ctx, c)
	if err != nil {
		return nil
	}
	sort.Strings(names)
	if a.names == nil {
		a.names = map[string]cachedNames{}
	}
	a.names[key] = cachedNames{names, time.Now()}
	return names
}

// ForgetNames drops the completion cache (after a create, delete or login).
func (a *App) ForgetNames() {
	a.mu.Lock()
	a.names = nil
	a.mu.Unlock()
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return ui.Dim.Render("—")
	}
	return s
}

func money(v float64, currency string) string {
	if currency == "" || strings.EqualFold(currency, "USD") {
		return fmt.Sprintf("$%.2f", v)
	}
	return fmt.Sprintf("%.2f %s", v, currency)
}
