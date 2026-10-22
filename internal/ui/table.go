package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

var (
	headerCell = lipgloss.NewStyle().Bold(true).Foreground(Blue500).PaddingRight(3)
	bodyCell   = lipgloss.NewStyle().PaddingRight(3)
)

// Table renders rows under a header rule, without boxes. Cells may carry
// their own styling (State, Dim); widths account for it.
func Table(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(Dim).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerCell
			}
			return bodyCell
		})
	return t.Render() + "\n"
}

// rowBorder draws only horizontal rules: under the header and between rows.
var rowBorder = lipgloss.Border{Top: "─", Bottom: "─", Middle: "─"}

var rowRule = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#e5e7eb", Dark: "#374151"})

// RowTable renders rows of several lines each, with a faint rule between
// rows so each one reads as a unit, like the portal's tables.
func RowTable(headers []string, rows [][]string) string {
	t := table.New().
		Border(rowBorder).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderHeader(true).BorderRow(true).
		BorderStyle(rowRule).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerCell
			}
			return bodyCell
		})
	return strings.TrimRight(t.Render(), " \n") + "\n"
}

// Badge is a small coloured label, like the portal's tier badges.
func Badge(label string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#4ade80"})
	switch label {
	case "Enterprise":
		style = style.Foreground(lipgloss.AdaptiveColor{Light: "#7e22ce", Dark: "#c084fc"})
	case "High Performance":
		style = style.Foreground(Blue500)
	case "AI Optimized":
		style = style.Foreground(lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#fb923c"})
	}
	return style.Render(label)
}
