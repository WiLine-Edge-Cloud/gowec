package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var logo = []string{
	"   ____ _____ _      _____  _____",
	"  / __ `/ __ \\ | /| / / _ \\/ ___/",
	" / /_/ / /_/ / |/ |/ /  __/ /__  ",
	" \\__, /\\____/|__/|__/\\___/\\___/  ",
	"/____/                           ",
}

var logoShades = []lipgloss.Color{Blue300, Blue400, Blue500, Blue600, Blue700}

// Logo is the coloured name with its version, for the top of the help.
func Logo(version string) string {
	var b strings.Builder
	b.WriteString("\n")
	for i, line := range logo {
		styled := lipgloss.NewStyle().Foreground(logoShades[i]).Bold(true).Render(line)
		if i == len(logo)-1 {
			styled += " " + Dim.Render("v"+version)
		}
		b.WriteString(" " + styled + "\n")
	}
	return b.String()
}

// Banner is the shell's greeting: the name in the portal's blues, the
// version beside its last line, then the status line under it.
func Banner(version, status string) string {
	var b strings.Builder
	b.WriteString(Logo(version))
	b.WriteString(" " + Dim.Render("WEC cloud console") + "\n\n")
	b.WriteString("  " + status + "\n")
	b.WriteString("  " + Dim.Render("type help for commands · tab completes · exit or Ctrl-D to leave") + "\n\n")
	return b.String()
}
