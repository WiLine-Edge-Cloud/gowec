package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func machineCmd(r *run) *cobra.Command {
	return group("vm", "Virtual machines", []string{"vms", "machine", "machines", "instance", "instances"},
		machineListCmd(r), machineGetCmd(r), machineCreateCmd(r),
		machineActionCmd(r, "start", "Start a stopped VM", "Starting", "is running",
			func(c *wec.Client, cmd *cobra.Command, m string) (*wec.JobAccepted, error) {
				return c.StartMachine(cmd.Context(), m)
			}),
		machineStopCmd(r),
		machineActionCmd(r, "reboot", "Reboot a running VM", "Rebooting", "rebooted",
			func(c *wec.Client, cmd *cobra.Command, m string) (*wec.JobAccepted, error) {
				return c.RebootMachine(cmd.Context(), m)
			}),
		machineResizeCmd(r), machineRenameCmd(r), machineDeleteCmd(r),
	)
}

// machineArg completes a machine name as the first argument.
func machineArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("vm", listMachineNames)
}

func listMachineNames(ctx context.Context, c *wec.Client) ([]string, error) {
	machines, err := c.ListMachines(ctx, wec.ListOptions{})
	names := make([]string, len(machines))
	for i, m := range machines {
		names[i] = m.Name
	}
	return names, err
}

func machineListCmd(r *run) *cobra.Command {
	var status, name string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List VMs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			machines, err := c.ListMachines(cmd.Context(), wec.ListOptions{Name: name, Status: status})
			if err != nil {
				return err
			}
			ids := make([]string, len(machines))
			for i, m := range machines {
				ids[i] = m.ID
			}
			if r.output != "table" || r.quiet {
				return r.show(machines, ids, nil)
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Loading VMs…")
			views := machineViews(cmd.Context(), c, machines)
			spin.Stop()
			return r.show(machines, ids, func() string { return machineTable(views) })
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only VMs in this state (running, stopped, …)")
	cmd.Flags().StringVar(&name, "name", "", "only the VM with this name")
	_ = cmd.RegisterFlagCompletionFunc("status", fixed("running", "stopped", "starting", "stopping", "error"))
	return cmd
}

func memory(mb int) string {
	if mb%1024 == 0 {
		return fmt.Sprintf("%dg", mb/1024)
	}
	if mb > 1024 {
		return fmt.Sprintf("%.1fg", float64(mb)/1024)
	}
	return fmt.Sprintf("%dm", mb)
}

func machineGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <vm>",
		Aliases:           []string{"show", "describe"},
		Short:             "Show one VM (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			m, err := c.GetMachine(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if r.output != "table" || r.quiet {
				return r.show(m, []string{m.ID}, nil)
			}
			v := machineViews(cmd.Context(), c, []wec.Machine{*m})[0]
			return r.show(m, []string{m.ID}, func() string { return machineDetail(v) })
		},
	}
}

// machineDetail is one machine with everything the instances table shows.
func machineDetail(v machineView) string {
	now := time.Now()
	pairs := [][2]string{
		{"Name", ui.Bold.Render(v.Name) + " " + machineBadges(v.Machine)},
		{"Status", ui.State(v.State)},
		{"ID", ui.Dim.Render(v.ID)},
		{"System", machineSystem(v.Machine)},
		{"Resources", oneLine(machineResources(v)) + ui.Dim.Render("  "+v.ServiceOfferingName)},
		{"Usage 24h", oneLine(machineUsage(v))},
		{"Zone", zoneName(v.ZoneName)},
		{"Public IP", dash(v.PublicIP)},
	}
	for _, a := range v.Addresses {
		label := a.IPAddress + "  " + ui.Dim.Render(a.NetworkName)
		if a.IsDefault {
			label += ui.Dim.Render(" (default)")
		}
		pairs = append(pairs, [2]string{"Network", label})
	}
	backups := ui.Dim.Render("No backups")
	if v.BackupOfferingID != "" {
		backups = ui.Green.Render("✓") + " Daily" + ui.Dim.Render("  "+v.BackupOfferingName)
	}
	pairs = append(pairs, [2]string{"Backups", backups})
	if v.plan != nil {
		pairs = append(pairs,
			[2]string{"Price", oneLine(machinePrice(v.plan))},
			[2]string{"Renewal", oneLine(machineRenewal(v.plan, now))},
			[2]string{"Plan", ui.Dim.Render(planID(*v.plan))},
		)
	}
	pairs = append(pairs, [2]string{"Created", oneLine(created(v.Created, now))})
	if v.DeleteProtection {
		pairs = append(pairs, [2]string{"Protected", "delete protection is on"})
	}
	return ui.Pairs(pairs)
}

func machineSystem(m wec.Machine) string {
	if m.OSName == "" || m.OSName == m.TemplateName {
		return dash(m.TemplateName)
	}
	return m.OSName + ui.Dim.Render("  "+m.TemplateName)
}

// oneLine joins a two-line table cell for a key/value view.
func oneLine(cell string) string {
	parts := strings.Split(cell, "\n")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, "  ")
}

type machineAction func(c *wec.Client, cmd *cobra.Command, machine string) (*wec.JobAccepted, error)

func machineActionCmd(r *run, use, short, doing, done string, act machineAction) *cobra.Command {
	return &cobra.Command{
		Use:               use + " <vm>",
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			accepted, err := act(c, cmd, args[0])
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, doing+" "+args[0], args[0]+" "+done)
		},
	}
}

func machineStopCmd(r *run) *cobra.Command {
	var force bool
	cmd := machineActionCmd(r, "stop", "Stop a running VM", "Stopping", "is stopped",
		func(c *wec.Client, cmd *cobra.Command, m string) (*wec.JobAccepted, error) {
			return c.StopMachine(cmd.Context(), m, force)
		})
	cmd.Flags().BoolVar(&force, "force", false, "pull the plug instead of shutting down cleanly")
	return cmd
}

func machineResizeCmd(r *run) *cobra.Command {
	var size string
	cmd := &cobra.Command{
		Use:               "resize <vm> --size <size>",
		Short:             "Change a VM's size (it may cost more)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Resize %s to %s? The new size may cost more.", args[0], size),
				"resizing can spend"); err != nil {
				return err
			}
			accepted, err := c.ResizeMachine(cmd.Context(), args[0], size, r.idempotencyKey())
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Resizing "+args[0], args[0]+" is now "+size)
		},
	}
	cmd.Flags().StringVar(&size, "size", "", "the new size, by name or id (gowec size list)")
	_ = cmd.MarkFlagRequired("size")
	return cmd
}

func machineRenameCmd(r *run) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "rename <vm> --name <new-name>",
		Short:             "Rename a VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			accepted, err := c.RenameMachine(cmd.Context(), args[0], name)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitJob(cmd.Context(), c, accepted, "Renaming "+args[0], args[0]+" is now "+name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the new name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func machineDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <vm>",
		Aliases:           []string{"rm", "destroy"},
		Short:             "Delete a VM (one on a plan goes with its plan)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Delete %s? Its root disk is destroyed.", args[0]),
				"deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteMachine(cmd.Context(), args[0])
			if wec.IsCode(err, "use_plan_delete") {
				return r.planDeleteHint(cmd, c, args[0], err)
			}
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitJob(cmd.Context(), c, accepted, "Deleting "+args[0], args[0]+" is deleted")
		},
	}
}

// planDeleteHint explains a refusal to delete a machine that runs on a plan,
// and names the plan to delete instead. It never deletes the plan itself:
// that moves money, so it is the person's own command.
func (r *run) planDeleteHint(cmd *cobra.Command, c *wec.Client, machine string, refusal error) error {
	fmt.Fprintln(r.app.Err, ui.Warn(machine+" runs on a plan, so it is deleted with its plan."))
	m, err := c.GetMachine(cmd.Context(), machine)
	if err != nil {
		return refusal
	}
	plans, err := c.ListPlans(cmd.Context())
	if err != nil {
		return refusal
	}
	for _, p := range plans {
		if p.Ledger != nil && p.Ledger.VMID == m.ID && p.Ledger.PlanID != "" {
			fmt.Fprintln(r.app.Err, "  "+ui.Accent.Render("→ ")+r.app.Refs("gowec plan delete-now "+p.Ledger.PlanID)+
				ui.Dim.Render("   (its unused days go back to the wallet)"))
			return errAborted
		}
	}
	return refusal
}

func machineCreateCmd(r *run) *cobra.Command {
	var body wec.LaunchMachine
	var noAutoRenew bool
	var maxPrice float64
	cmd := &cobra.Command{
		Use:     "create --name <name> --zone <zone> --template <os> --size <size> --disk <gb> --network <network>",
		Aliases: []string{"launch", "new"},
		Short:   "Launch a VM on a prepaid plan (charges the wallet)",
		Long: "Launch a VM on a prepaid plan. gowec quotes the price first and asks before\n" +
			"charging the wallet; in scripts, pass --yes, or --max-price to refuse anything dearer.",
		Example: "  gowec vm create --name web-1 --zone princeton --template \"Ubuntu 24.04 LTS\" \\\n" +
			"    --size wa-gnp-1.1c-0.5gb --disk 20 --network Default-Subnet --term 12 --backups",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if body.TermMonths != 1 && body.TermMonths != 12 && body.TermMonths != 24 {
				return usagef("--term must be 1, 12 or 24 months")
			}
			body.AutoRenew = !noAutoRenew

			spin := ui.StartSpinner(r.app.Err, r.tty(), "Pricing…")
			quote, err := c.QuotePlan(cmd.Context(), body.Size, body.DiskGB)
			spin.Stop()
			if err != nil {
				return err
			}
			term, err := quote.Term(body.TermMonths)
			if err != nil {
				return err
			}
			if !term.Available {
				return fmt.Errorf("the %d-month term isn't available for this size: %s", body.TermMonths, term.Reason)
			}
			body.QuotedTotal = term.Total(body.Backups, body.PublicAddress)

			if r.output == "table" && !r.quiet {
				fmt.Fprint(r.app.Err, launchSummary(body, quote, term))
			}
			if maxPrice > 0 && body.QuotedTotal > maxPrice {
				return fmt.Errorf("the price, %s, is above --max-price %s; nothing was charged",
					money(body.QuotedTotal, quote.Currency), money(maxPrice, quote.Currency))
			}
			if maxPrice <= 0 {
				question := fmt.Sprintf("Launch %s and charge %s to the wallet?", body.Name,
					money(body.QuotedTotal, quote.Currency))
				if err := r.confirm(question, "launching charges the wallet"); err != nil {
					return err
				}
			}

			accepted, err := c.CreateLaunch(cmd.Context(), body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			if r.detach {
				return r.show(accepted, []string{accepted.LaunchID}, func() string {
					return ui.Success("Launching "+body.Name+" · launch "+accepted.LaunchID) + "\n" +
						ui.Dim.Render(r.app.Refs("  follow it with: gowec launch wait "+accepted.LaunchID)) + "\n"
				})
			}
			return r.followLaunch(cmd, c, accepted.LaunchID)
		},
	}
	f := cmd.Flags()
	f.StringVar(&body.Name, "name", "", "the VM's name (letters, digits, - and _)")
	f.StringVar(&body.Zone, "zone", "", "zone name or id (gowec zone list)")
	f.StringVar(&body.Template, "template", "", "operating system, name or id (gowec template list)")
	f.StringVar(&body.Size, "size", "", "size name or id (gowec size list)")
	f.IntVar(&body.DiskGB, "disk", 0, "root disk in GB, one of gowec disk list")
	f.StringVar(&body.Network, "network", "", "network name or id")
	f.IntVar(&body.TermMonths, "term", 1, "the plan's term in months: 1, 12 or 24")
	f.BoolVar(&body.Backups, "backups", false, "daily backups at 03:00 UTC, the last 2 kept")
	f.BoolVar(&body.PublicAddress, "public-address", false, "a new public address on the VM's network")
	f.StringVar(&body.SSHKey, "ssh-key", "", "a registered SSH key's name (gowec ssh-key list)")
	f.BoolVar(&noAutoRenew, "no-auto-renew", false, "end the plan at its term instead of renewing it")
	f.Float64Var(&maxPrice, "max-price", 0, "launch without asking if the price is at most this; refuse otherwise")
	for _, name := range []string{"name", "zone", "template", "size", "disk", "network"} {
		_ = cmd.MarkFlagRequired(name)
	}
	_ = cmd.RegisterFlagCompletionFunc("term", fixed("1", "12", "24"))
	return cmd
}

func sizeLabel(size, service string) string {
	if service == "" || strings.EqualFold(service, size) {
		return size
	}
	return size + "  " + ui.Dim.Render(service)
}

func launchSummary(body wec.LaunchMachine, quote *wec.PlanQuote, term *wec.QuoteTerm) string {
	months := "1 month"
	if body.TermMonths > 1 {
		months = fmt.Sprintf("%d months", body.TermMonths)
	}
	pairs := [][2]string{
		{"VM", ui.Bold.Render(body.Name)},
		{"Size", sizeLabel(body.Size, quote.ServiceName)},
		{"Zone", body.Zone},
		{"System", body.Template},
		{"Disk", fmt.Sprintf("%d GB", body.DiskGB)},
		{"Term", months},
	}
	var extras []string
	if body.Backups {
		extras = append(extras, fmt.Sprintf("backups %s", money(term.AddonPrices["backup"], quote.Currency)))
	}
	if body.PublicAddress {
		extras = append(extras, fmt.Sprintf("public address %s", money(term.AddonPrices["ip_address"], quote.Currency)))
	}
	if len(extras) > 0 {
		pairs = append(pairs, [2]string{"Add-ons", strings.Join(extras, ", ")})
	}
	total := ui.Bold.Render(money(body.QuotedTotal, quote.Currency))
	if body.TermMonths > 1 {
		total += ui.Dim.Render(fmt.Sprintf("  (%s a month)",
			money(body.QuotedTotal/float64(body.TermMonths), quote.Currency)))
	}
	pairs = append(pairs, [2]string{"Total", total})
	return "\n" + ui.Pairs(pairs) + "\n"
}
