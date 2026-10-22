// Package ui is gowec's look: colours, the banner, tables, the spinner and prompts.
//
// lipgloss picks the colour profile from stdout, so piped output and NO_COLOR
// come out plain without any checks here.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The portal's blues (Tailwind blue, the dashboard's --primary is blue-500).
var (
	Blue100 = lipgloss.Color("#dbeafe")
	Blue200 = lipgloss.Color("#bfdbfe")
	Blue300 = lipgloss.Color("#93c5fd")
	Blue400 = lipgloss.Color("#60a5fa")
	Blue500 = lipgloss.Color("#3b82f6")
	Blue600 = lipgloss.Color("#2563eb")
	Blue700 = lipgloss.Color("#1d4ed8")
)

var (
	Accent  = lipgloss.NewStyle().Foreground(Blue500)
	Bold    = lipgloss.NewStyle().Bold(true)
	Dim     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"})
	Green   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"})
	Yellow  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#facc15"})
	Red     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f87171"})
	Heading = lipgloss.NewStyle().Bold(true).Foreground(Blue500)
)

// Success, Warn and Fail mark a line's outcome.
func Success(msg string) string { return Green.Render("✓") + " " + msg }
func Warn(msg string) string    { return Yellow.Render("!") + " " + msg }
func Fail(msg string) string    { return Red.Render("✗") + " " + msg }

// State shows a resource state as an icon and a colour: running or ready
// green, stopped or gone grey, moving yellow, broken red.
func State(s string) string {
	switch strings.ToLower(s) {
	case "":
		return Dim.Render("—")
	case "running":
		return Green.Render("▶ " + s)
	case "ready", "allocated", "succeeded", "active", "implemented", "enabled", "setup", "backedup", "open", "new":
		return Green.Render("● " + s)
	case "stopped":
		return Dim.Render("■ " + s)
	case "destroyed", "expunged", "expunging", "idle", "ended", "closed", "released", "cancelled", "removed", "merged":
		return Dim.Render("○ " + s)
	case "starting", "stopping", "migrating", "pending", "creating", "allocating", "committing", "backingup",
		"restoring", "pending reminder", "pending close", "removing", "buying":
		return Yellow.Render("◌ " + s)
	case "error", "failed", "unknown":
		return Red.Render("✗ " + s)
	}
	return s
}

// Pairs renders "key  value" lines with the keys aligned and dimmed.
func Pairs(pairs [][2]string) string {
	width := 0
	for _, p := range pairs {
		width = max(width, len(p[0]))
	}
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(Dim.Render(p[0]+strings.Repeat(" ", width-len(p[0]))) + "  " + p[1] + "\n")
	}
	return b.String()
}
