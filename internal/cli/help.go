package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
)

// renderHelp is gowec's help screen: the name and what it does, how to call
// it, its commands by group, its flags, and examples.
func renderHelp(app *App, cmd *cobra.Command) string {
	var b strings.Builder
	isRoot := !cmd.HasParent()

	if isRoot && !app.InShell {
		b.WriteString(ui.Logo(app.Version))
	}
	title := ui.Bold.Render(cmd.CommandPath())
	if cmd.Short != "" {
		title += ui.Dim.Render("  ·  ") + cmd.Short
	}
	b.WriteString("\n " + title + "\n")
	if cmd.Long != "" && cmd.Long != cmd.Short {
		b.WriteString("\n" + indent(cmd.Long, " ") + "\n")
	}

	section(&b, "Usage", "   "+usageLine(cmd))
	if len(cmd.Aliases) > 0 {
		section(&b, "Aliases", "   "+ui.Dim.Render(strings.Join(append([]string{cmd.Name()}, cmd.Aliases...), ", ")))
	}

	children := visible(cmd.Commands())
	if len(children) > 0 {
		width := 0
		for _, c := range children {
			width = max(width, len(c.Name()))
		}
		groups := cmd.Groups()
		if len(groups) == 0 {
			section(&b, "Commands", commandList(children, width))
		}
		for _, g := range groups {
			var in []*cobra.Command
			for _, c := range children {
				if c.GroupID == g.ID {
					in = append(in, c)
				}
			}
			if len(in) > 0 {
				section(&b, strings.TrimSuffix(g.Title, ":"), commandList(in, width))
			}
		}
		var rest []*cobra.Command
		for _, c := range children {
			if c.GroupID == "" && len(groups) > 0 {
				rest = append(rest, c)
			}
		}
		if len(rest) > 0 {
			section(&b, "More", commandList(rest, width))
		}
	}

	if cmd.Example != "" {
		section(&b, "Examples", examples(cmd.Example, app.InShell))
	}
	if list := flagList(cmd.LocalNonPersistentFlags()); list != "" && !isRoot {
		section(&b, "Flags", list)
	}
	if isRoot {
		section(&b, "Global flags", flagList(cmd.PersistentFlags()))
	} else if inherited := cmd.InheritedFlags(); inherited.HasAvailableFlags() {
		section(&b, "Global flags", "   "+ui.Dim.Render(flagNames(inherited, app.InShell)))
	}

	next := "gowec <command> --help"
	if !isRoot && len(children) > 0 {
		next = cmd.CommandPath() + " <command> --help"
	}
	if len(children) > 0 || isRoot {
		b.WriteString("\n " + ui.Dim.Render("Run ") + ui.Accent.Render(next) + ui.Dim.Render(" for more about a command.") + "\n")
	}
	return app.Refs(b.String()) + "\n"
}

func installHelp(app *App, root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		fmt.Fprint(cmd.OutOrStdout(), renderHelp(app, cmd))
	})
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		_, err := io.WriteString(cmd.OutOrStderr(), renderHelp(app, cmd))
		return err
	})
}

func section(b *strings.Builder, title, body string) {
	b.WriteString("\n " + ui.Heading.Render(strings.ToUpper(title)) + "\n" + body + "\n")
}

func usageLine(cmd *cobra.Command) string {
	if !cmd.HasParent() {
		return ui.Accent.Render("gowec") + " <resource> <action> [target] [flags]"
	}
	if cmd.HasAvailableSubCommands() && !cmd.Runnable() {
		return ui.Accent.Render(cmd.CommandPath()) + " <action> [flags]"
	}
	use := cmd.UseLine()
	path := cmd.CommandPath()
	return ui.Accent.Render(path) + strings.TrimPrefix(use, path)
}

func visible(cmds []*cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, c := range cmds {
		if c.IsAvailableCommand() && c.Name() != "help" {
			out = append(out, c)
		}
	}
	return out
}

func commandList(cmds []*cobra.Command, width int) string {
	var lines []string
	for _, c := range cmds {
		line := "   " + ui.Accent.Render(pad(c.Name(), width)) + "   " + c.Short
		if len(c.Aliases) > 0 {
			line += ui.Dim.Render("  (" + strings.Join(c.Aliases, ", ") + ")")
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func flagList(fs *pflag.FlagSet) string {
	type row struct {
		flag, typ, usage string
		required         bool
	}
	var rows []row
	width := 0
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		r := row{flag: "    --" + f.Name, usage: f.Usage}
		if f.Shorthand != "" {
			r.flag = "-" + f.Shorthand + ", --" + f.Name
		}
		if t := f.Value.Type(); t != "bool" {
			r.typ = t
		}
		if _, required := f.Annotations[cobra.BashCompOneRequiredFlag]; required {
			r.required = true
			r.usage += " " + ui.Yellow.Render("(required)")
		} else if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "[]" {
			r.usage += ui.Dim.Render(" (default " + f.DefValue + ")")
		}
		width = max(width, len(r.flag)+1+len(r.typ))
		rows = append(rows, r)
	})
	// Required flags first: they are what a command can't run without.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].required && !rows[j].required })
	lines := make([]string, len(rows))
	for i, r := range rows {
		gap := width - len(r.flag) - 1 - len(r.typ) + 3
		lines[i] = "   " + ui.Accent.Render(r.flag) + " " + ui.Dim.Render(r.typ) + strings.Repeat(" ", gap) + r.usage
	}
	return strings.Join(lines, "\n")
}

func flagNames(fs *pflag.FlagSet, inShell bool) string {
	var names []string
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		if f.Shorthand != "" {
			names = append(names, "-"+f.Shorthand+"/--"+f.Name)
		} else {
			names = append(names, "--"+f.Name)
		}
	})
	more := "gowec --help"
	if inShell {
		more = "help"
	}
	return strings.Join(names, "  ") + "   (" + more + ")"
}

// examples marks each command with the prompt it is typed at: the console's
// in the console, a shell's otherwise.
func examples(text string, inShell bool) string {
	prompt := "$ "
	if inShell {
		prompt = "› "
	}
	var lines []string
	for _, line := range strings.Split(strings.Trim(text, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			lines = append(lines, "")
		case strings.HasPrefix(trimmed, "gowec") || strings.HasPrefix(trimmed, "echo"):
			if strings.Contains(trimmed, "| gowec") {
				lines = append(lines, "   "+ui.Dim.Render("$ ")+trimmed) // a shell pipeline either way
			} else {
				lines = append(lines, "   "+ui.Dim.Render(prompt)+trimmed)
			}
		default: // a continued line
			lines = append(lines, "   "+ui.Dim.Render("  ")+trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-len(s), 0))
}
