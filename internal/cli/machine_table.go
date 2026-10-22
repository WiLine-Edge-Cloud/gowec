package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

// machineView is a machine with what the instances table joins onto it:
// its plan, its root disk and its use over the last day.
type machineView struct {
	wec.Machine
	plan   *wec.Plan
	diskGB float64
	usage  *wec.MachineUsage
}

// machineViews joins plans, root volumes and usage onto machines, the way
// the portal's instances table does. Each extra is fail-open: a key without
// billing view, or a metrics hiccup, leaves its columns blank, not the list.
func machineViews(ctx context.Context, c *wec.Client, machines []wec.Machine) []machineView {
	ids := make([]string, len(machines))
	for i, m := range machines {
		ids[i] = m.ID
	}
	var (
		wg    sync.WaitGroup
		plans map[string]*wec.Plan
		disks = map[string]float64{}
		usage *wec.MachinesUsage
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		if all, err := c.ListPlans(ctx); err == nil {
			plans = wec.PlansByMachine(all)
		}
	}()
	go func() {
		defer wg.Done()
		if vols, err := c.ListVolumes(ctx, wec.ListOptions{}); err == nil {
			for _, v := range vols {
				if strings.EqualFold(v.Type, "root") && v.MachineID != "" {
					disks[v.MachineID] = v.SizeGB
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		if len(ids) > 0 {
			usage, _ = c.GetMachinesUsage(ctx, ids)
		}
	}()
	wg.Wait()

	views := make([]machineView, len(machines))
	for i, m := range machines {
		v := machineView{Machine: m, plan: plans[m.ID], diskGB: disks[m.ID]}
		if m.RootDiskGB > 0 {
			v.diskGB = float64(m.RootDiskGB)
		}
		if usage != nil {
			if u, ok := usage.Machines[m.ID]; ok {
				v.usage = &u
			}
		}
		views[i] = v
	}
	return views
}

// column is one table column; columns with a higher drop number go first
// when the terminal is too narrow. Zero never drops.
type column struct {
	header string
	drop   int
	cell   func(machineView) string
}

func machineColumns(now time.Time) []column {
	return []column{
		{"NAME", 0, func(v machineView) string {
			return ui.Bold.Render(v.Name) + " " + machineBadges(v.Machine) + "\n" + ui.Dim.Render(dash(v.OSName))
		}},
		{"RESOURCES", 0, func(v machineView) string {
			return machineResources(v)
		}},
		{"USAGE 24H", 5, func(v machineView) string { return machineUsage(v) }},
		{"STATUS", 0, func(v machineView) string { return ui.State(v.State) }},
		{"ZONE", 7, func(v machineView) string { return zoneName(v.ZoneName) }},
		{"PUBLIC IP", 3, func(v machineView) string {
			if v.PublicIP == "" {
				return ui.Dim.Render("No address")
			}
			return v.PublicIP
		}},
		{"BACKUPS", 6, func(v machineView) string {
			if v.BackupOfferingID == "" {
				return ui.Dim.Render("No backups")
			}
			return ui.Green.Render("✓") + " Daily"
		}},
		{"PRICE", 2, func(v machineView) string { return machinePrice(v.plan) }},
		{"RENEWAL", 4, func(v machineView) string { return machineRenewal(v.plan, now) }},
		{"CREATED", 8, func(v machineView) string { return created(v.Created, now) }},
	}
}

// machineTable renders the instances table, dropping the least useful
// columns until it fits the terminal.
func machineTable(views []machineView) string {
	if len(views) == 0 {
		return ui.Dim.Render("No VMs.") + "\n"
	}
	cols := machineColumns(time.Now())
	if allSameZone(views) {
		// One zone says nothing per row; drop it first, as the portal does.
		for i := range cols {
			if cols[i].header == "ZONE" {
				cols[i].drop = 9
			}
		}
	}
	width := terminalWidth()
	for {
		out := renderColumns(cols, views)
		if width == 0 || lipgloss.Width(out) <= width {
			return out + "\n" + machineFooter(views)
		}
		worst := -1
		for i, c := range cols {
			if c.drop > 0 && (worst < 0 || c.drop > cols[worst].drop) {
				worst = i
			}
		}
		if worst < 0 {
			return out + "\n" + machineFooter(views)
		}
		cols = append(cols[:worst], cols[worst+1:]...)
	}
}

func renderColumns(cols []column, views []machineView) string {
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.header
	}
	rows := make([][]string, len(views))
	for r, v := range views {
		rows[r] = make([]string, len(cols))
		for i, c := range cols {
			rows[r][i] = c.cell(v)
		}
	}
	return ui.RowTable(headers, rows)
}

func machineFooter(views []machineView) string {
	counts := map[string]int{}
	for _, v := range views {
		counts[strings.ToLower(v.State)]++
	}
	parts := []string{fmt.Sprintf("%d VMs", len(views))}
	states := make([]string, 0, len(counts))
	for s := range counts {
		states = append(states, s)
	}
	sort.Strings(states)
	for _, s := range states {
		parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
	}
	return ui.Dim.Render(strings.Join(parts, " · ")) + "\n"
}

func terminalWidth() int {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

func allSameZone(views []machineView) bool {
	for _, v := range views[1:] {
		if v.ZoneName != views[0].ZoneName {
			return false
		}
	}
	return true
}

// machineBadges are the portal's tier and CPU vendor badges.
func machineBadges(m wec.Machine) string {
	tier := machineTier(m)
	var vendor string
	switch {
	case strings.HasPrefix(m.ServiceOfferingName, "wa"):
		vendor = "AMD"
	case strings.HasPrefix(m.ServiceOfferingName, "wi"):
		vendor = "Intel"
	}
	out := ui.Badge(tier)
	if vendor != "" {
		out += " " + ui.Dim.Render(vendor)
	}
	return out
}

// machineTier mirrors the portal's performance tiers (getPerformanceTier),
// with GPU sizes shown as AI Optimized.
func machineTier(m wec.Machine) string {
	name := strings.ToLower(m.ServiceOfferingName)
	switch {
	case strings.Contains(name, "gpu"):
		return "AI Optimized"
	case m.CPUCount >= 16:
		return "Enterprise"
	case m.CPUCount >= 8 && m.MemoryMB >= 32*1024:
		return "High Performance"
	}
	return "Basic"
}

func machineResources(v machineView) string {
	top := fmt.Sprintf("%d vCPU · %s", v.CPUCount, gb(float64(v.MemoryMB)/1024))
	var sub []string
	if v.diskGB > 0 {
		sub = append(sub, fmt.Sprintf("%g GB disk", v.diskGB))
	}
	if v.CPUSpeedMHz > 0 {
		sub = append(sub, fmt.Sprintf("%d MHz", v.CPUSpeedMHz))
	}
	return top + "\n" + ui.Dim.Render(strings.Join(sub, " · "))
}

func gb(v float64) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", v), "0"), ".") + " GB"
}

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

const sparkWidth = 12

// machineUsage is a CPU sparkline for the day with the latest CPU and
// memory readings, like the portal's Utilization 24h column.
func machineUsage(v machineView) string {
	if v.usage == nil || v.usage.CPUNow == nil {
		return ui.Dim.Render(strings.Repeat("─", sparkWidth) + " — cpu\n" +
			strings.Repeat(" ", sparkWidth) + " — mem")
	}
	mem := "— mem"
	if v.usage.MemNow != nil {
		mem = fmt.Sprintf("%.0f%% mem", *v.usage.MemNow)
	}
	return ui.Accent.Render(sparkline(v.usage.CPU)) + fmt.Sprintf(" %.0f%%", *v.usage.CPUNow) + ui.Dim.Render(" cpu") +
		"\n" + strings.Repeat(" ", sparkWidth+1) + ui.Dim.Render(mem)
}

// sparkline squeezes the buckets into sparkWidth characters. The scale starts
// at 10%, so an idle machine's noise stays flat instead of filling the chart.
func sparkline(buckets []*float64) string {
	if len(buckets) == 0 {
		return strings.Repeat(" ", sparkWidth)
	}
	points := make([]float64, sparkWidth)
	have := make([]bool, sparkWidth)
	top := 10.0
	for i := range points {
		from := i * len(buckets) / sparkWidth
		to := max((i+1)*len(buckets)/sparkWidth, from+1)
		var sum float64
		var n int
		for _, b := range buckets[from:min(to, len(buckets))] {
			if b != nil {
				sum += *b
				n++
			}
		}
		if n > 0 {
			points[i], have[i] = sum/float64(n), true
			top = math.Max(top, points[i])
		}
	}
	var b strings.Builder
	for i, p := range points {
		if !have[i] {
			b.WriteRune(' ')
			continue
		}
		level := int(math.Round(p / top * float64(len(sparkLevels)-1)))
		b.WriteRune(sparkLevels[min(max(level, 0), len(sparkLevels)-1)])
	}
	return b.String()
}

// zoneName is the portal's place name for a zone (zone-presentation.ts).
func zoneName(zone string) string {
	z := strings.ToLower(zone)
	for key, place := range map[string]string{
		"sao-paulo": "São Paulo, Brazil", "roseville": "Roseville, USA",
		"princeton": "Princeton, USA", "philadelphia": "Philadelphia, USA",
	} {
		if strings.Contains(z, key) {
			return place
		}
	}
	words := strings.Fields(strings.ReplaceAll(zone, "-", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return dash(strings.Join(words, " "))
}

// machinePrice is a month of the machine's plan, add-ons included; a longer
// term also shows what the whole term cost.
func machinePrice(p *wec.Plan) string {
	if p == nil {
		return ui.Dim.Render("—")
	}
	monthly, ok := p.Monthly()
	if !ok {
		return ui.Dim.Render("—")
	}
	top := ui.Bold.Render(money(monthly, "")) + ui.Dim.Render("/mo")
	sub := "Monthly"
	switch {
	case p.TermMonths == 12:
		sub = money(p.Paid(), "") + "/yr"
	case p.TermMonths > 12 && p.TermMonths%12 == 0:
		sub = fmt.Sprintf("%s/%d yrs", money(p.Paid(), ""), p.TermMonths/12)
	case p.TermMonths > 1:
		sub = fmt.Sprintf("%s/%d mo", money(p.Paid(), ""), p.TermMonths)
	}
	return top + "\n" + ui.Dim.Render(sub)
}

// machineRenewal says when the plan renews, or when it ends and takes the
// machine with it (amber, as in the portal).
func machineRenewal(p *wec.Plan, now time.Time) string {
	if p == nil {
		return ui.Dim.Render("—")
	}
	ends, ok := parseTime(p.EndsAt)
	if !ok {
		return ui.Dim.Render("—")
	}
	days := int(math.Ceil(ends.Sub(now).Hours() / 24))
	when := fmt.Sprintf("in %d days", days)
	switch {
	case days == 1:
		when = "tomorrow"
	case days == 0:
		when = "today"
	case days < 0:
		when = "ended"
	}
	date := ends.Local().Format("Jan 2, 2006")
	if p.AutoRenew {
		return "Renews " + date + "\n" + ui.Dim.Render(when)
	}
	if days >= 0 {
		when = "removed then · " + when
	}
	return ui.Yellow.Render("Ends "+date) + "\n" + ui.Dim.Render(when)
}

func created(ts string, now time.Time) string {
	at, ok := parseTime(ts)
	if !ok {
		return dash(ts)
	}
	return at.Local().Format("Jan 2, 2006") + "\n" + ui.Dim.Render(ago(now.Sub(at)))
}

// parseTime reads the API's timestamps: RFC 3339, and CloudStack's own form.
func parseTime(ts string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02"} {
		if at, err := time.Parse(layout, ts); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

// day is a timestamp's date, as the portal writes it.
func day(ts string) string {
	at, ok := parseTime(ts)
	if !ok {
		if len(ts) >= 10 {
			return ts[:10]
		}
		return ts
	}
	return at.Local().Format("Jan 2, 2006")
}

func ago(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "a day ago"
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
