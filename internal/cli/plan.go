package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func planCmd(r *run) *cobra.Command {
	return group("plan", "Prepaid plans: what VMs run on", []string{"plans"},
		planListCmd(r), planQuoteCmd(r), planUpgradeCmd(r), planAddonCmd(r),
		planDeleteNowCmd(r))
}

func planListCmd(r *run) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the account's plans that are still running",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			listed, err := c.ListPlans(cmd.Context())
			if err != nil {
				return err
			}
			plans := listed[:0:0]
			for _, p := range listed {
				if all || !p.Ended() {
					plans = append(plans, p)
				}
			}
			names := map[string]string{}
			if r.output == "table" && !r.quiet {
				if machines, err := c.ListMachines(cmd.Context(), wec.ListOptions{}); err == nil {
					for _, m := range machines {
						names[m.ID] = m.Name
					}
				}
			}
			ids := make([]string, len(plans))
			for i, p := range plans {
				ids[i] = planID(p)
			}
			return r.show(plans, ids, func() string {
				if len(plans) == 0 {
					return ui.Dim.Render("No plans.") + "\n"
				}
				now := time.Now()
				rows := make([][]string, len(plans))
				for i, p := range plans {
					status, vm := p.Status, p.VMID
					if p.Ledger != nil {
						status, vm = p.Ledger.Status, p.Ledger.VMID
					}
					machine := names[vm]
					if machine == "" {
						machine = planMachineName(p)
					}
					rows[i] = []string{ui.Bold.Render(dash(machine)) + "\n" + ui.Dim.Render(planKind(p)), ui.State(status),
						planPriceCell(&p), machineRenewal(&p, now), ui.Dim.Render(planID(p))}
				}
				return ui.RowTable([]string{"VM / PLAN", "STATUS", "PRICE", "RENEWAL", "ID"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include cancelled and ended plans")
	return cmd
}

// planPriceCell is a plan's price a month, and what its whole term cost.
// Unlike machinePrice it counts the plan alone: add-on plans have rows of their own.
func planPriceCell(p *wec.Plan) string {
	if p.Charged == nil || p.TermMonths == 0 {
		return ui.Dim.Render("—")
	}
	top := ui.Bold.Render(money(*p.Charged/float64(p.TermMonths), "")) + ui.Dim.Render("/mo")
	if p.TermMonths == 1 {
		return top + "\n" + ui.Dim.Render("Monthly")
	}
	return top + "\n" + ui.Dim.Render(money(*p.Charged, "")+" for "+planMonths(p.TermMonths))
}

// planID is the id plan commands take: the portal's plan, else billing's enrollment.
func planID(p wec.Plan) string {
	if p.Ledger != nil && p.Ledger.PlanID != "" {
		return p.Ledger.PlanID
	}
	return p.ID
}

// planParts splits the portal's plan name, "<machine> · <what> · … · ref <x>".
func planParts(p wec.Plan) []string {
	name := p.DisplayName
	if name == "" {
		name = p.DealName
	}
	return strings.Split(name, " · ")
}

// planMachineName is the machine a plan was bought for, from its name; used
// when the machine is gone or the plan's link to it is missing.
func planMachineName(p wec.Plan) string {
	if parts := planParts(p); len(parts) > 1 {
		return ui.Dim.Render(parts[0])
	}
	return ""
}

// planKind says what the plan pays for: a machine, an add-on or a data volume.
func planKind(p wec.Plan) string {
	parts := planParts(p)
	switch {
	case p.Ledger != nil:
		kind := "VM"
		if len(parts) > 1 {
			kind += " " + parts[1]
		}
		if p.Ledger.DiskGB > 0 {
			kind += fmt.Sprintf(" · %g GB", p.Ledger.DiskGB)
		}
		return kind
	case p.Addon != nil && len(parts) > 1:
		return parts[1]
	case p.Addon != nil:
		return "add-on"
	case p.VolumePlan != nil:
		return "data volume"
	case len(parts) > 1:
		return parts[1]
	case parts[0] != "":
		return parts[0] // an older bundle, named as a whole
	}
	return "plan"
}

func planQuoteCmd(r *run) *cobra.Command {
	var size string
	var disk int
	cmd := &cobra.Command{
		Use:   "quote --size <size> --disk <gb>",
		Short: "What a VM of this size costs, per term",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			q, err := c.QuotePlan(cmd.Context(), size, disk)
			if err != nil {
				return err
			}
			return r.show(q, nil, func() string {
				head := ui.Bold.Render(q.ServiceName) + ui.Dim.Render(fmt.Sprintf(" · %g GB · %s", q.DiskGB, zoneName(q.City))) + "\n\n"
				rows := make([][]string, 0, len(q.Terms))
				for _, t := range q.Terms {
					if !t.Available {
						rows = append(rows, []string{fmt.Sprintf("%d mo", t.TermMonths),
							ui.Dim.Render("not offered " + t.Reason), dash(""), dash(""), dash("")})
						continue
					}
					rows = append(rows, []string{fmt.Sprintf("%d mo", t.TermMonths), money(t.PackagePrice, q.Currency),
						money(t.MonthlyEffective, q.Currency), money(t.AddonPrices["backup"], q.Currency),
						money(t.AddonPrices["ip_address"], q.Currency)})
				}
				return head + ui.Table([]string{"TERM", "PRICE", "A MONTH", "+BACKUPS", "+ADDRESS"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&size, "size", "", "size name or id (gowec size list)")
	cmd.Flags().IntVar(&disk, "disk", 0, "root disk in GB")
	_ = cmd.MarkFlagRequired("size")
	_ = cmd.MarkFlagRequired("disk")
	return cmd
}

func planDeleteNowCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "delete-now <plan-id>",
		Short: "Delete a plan and its VM now (refunds the unused days)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm("Delete this plan and its VM now? Data volumes are kept, detached.",
				"deleting a plan can't be undone"); err != nil {
				return err
			}
			out, err := c.DeletePlanNow(cmd.Context(), args[0], r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.show(out, []string{args[0]}, func() string {
				msg := "The plan and its VM are being deleted"
				refund, _ := out["refund"].(float64)
				addons, _ := out["addons_refund"].(float64)
				if refund+addons > 0 {
					msg += fmt.Sprintf("; %s goes back to the wallet once the VM is gone (about a minute)", money(refund+addons, ""))
				}
				vm, _ := out["vm_id"].(string)
				return ui.Success(msg) + "\n" + ui.Dim.Render("  VM "+dash(vm)) + "\n"
			})
		},
	}
}

// machinePlan finds the running machine plan target names: its plan id, its
// enrollment id, or the machine it runs (name or id). It returns the
// machine's name with it.
func machinePlan(cmd *cobra.Command, c *wec.Client, target string) (*wec.Plan, string, error) {
	plans, err := c.ListPlans(cmd.Context())
	if err != nil {
		return nil, "", err
	}
	for i := range plans {
		p := &plans[i]
		if p.Ledger != nil && !p.Ended() && (p.Ledger.PlanID == target || p.ID == target) {
			name := target
			if parts := planParts(*p); len(parts) > 1 {
				name = parts[0]
			}
			return p, name, nil
		}
	}
	m, err := c.GetMachine(cmd.Context(), target)
	var apiErr *wec.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 && wec.IsID(target) {
		return nil, "", fmt.Errorf("no running plan or VM has the id %s: see gowec plan list", target)
	}
	if err != nil {
		return nil, "", err
	}
	if p := wec.PlansByMachine(plans)[m.ID]; p != nil {
		return p, m.Name, nil
	}
	return nil, "", fmt.Errorf("%s doesn't run on a plan", m.Name)
}

// priceMoved explains a write refused because the price moved since it was
// quoted. It never re-sends at the new price: that is the person's call.
func priceMoved(err error) error {
	var apiErr *wec.APIError
	if errors.As(err, &apiErr) {
		if v, ok := apiErr.Extra["quoted_total"].(float64); ok {
			return fmt.Errorf("the price is now %s, not what was quoted; nothing was charged. Run the command again to review it",
				money(v, ""))
		}
	}
	return err
}

func planMonths(n int) string {
	if n == 1 {
		return "1 month"
	}
	return fmt.Sprintf("%d months", n)
}

func planUpgradeCmd(r *run) *cobra.Command {
	var term int
	var maxPrice float64
	cmd := &cobra.Command{
		Use:   "upgrade <plan|vm> --term <months>",
		Short: "Move a plan to a longer term (charges the difference)",
		Long: "Move a running VM's plan to a longer term. You pay the longer term less what is\n" +
			"left of this one. gowec prices it first and asks before charging the wallet; in\n" +
			"scripts, pass --yes, or --max-price to refuse anything dearer.",
		Example:           "  gowec plan upgrade web-1 --term 12",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			if term != 12 && term != 24 {
				return usagef("--term must be 12 or 24 months")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Pricing…")
			p, machine, err := machinePlan(cmd, c, args[0])
			var price *wec.UpgradeQuote
			if err == nil {
				price, err = c.QuoteUpgrade(cmd.Context(), p.ID, term)
			}
			spin.Stop()
			if err != nil {
				return err
			}

			if r.output == "table" && !r.quiet {
				fmt.Fprint(r.app.Err, upgradeSummary(machine, price))
			}
			if maxPrice > 0 && price.Due > maxPrice {
				return fmt.Errorf("the price, %s, is above --max-price %s; nothing was charged",
					money(price.Due, ""), money(maxPrice, ""))
			}
			if maxPrice <= 0 {
				question := fmt.Sprintf("Move %s to %s and charge %s to the wallet?", machine, planMonths(term), money(price.Due, ""))
				if err := r.confirm(question, "upgrading charges the wallet"); err != nil {
					return err
				}
			}

			accepted, err := c.UpgradePlan(cmd.Context(), p.ID, term, price.Due, r.idempotencyKey())
			if wec.IsCode(err, "plan_upgrade_price_changed") {
				return priceMoved(err)
			}
			if err != nil {
				return err
			}
			if r.detach {
				return r.show(accepted, []string{accepted.UpgradeID}, func() string {
					return ui.Success("Upgrading "+machine+" · upgrade "+accepted.UpgradeID) + "\n" +
						ui.Dim.Render(r.app.Refs("  follow it with: gowec upgrade wait "+accepted.UpgradeID)) + "\n"
				})
			}
			return r.followUpgrade(cmd, c, accepted.UpgradeID)
		},
	}
	cmd.Flags().IntVar(&term, "term", 0, "the longer term in months: 12 or 24")
	cmd.Flags().Float64Var(&maxPrice, "max-price", 0, "upgrade without asking if the price is at most this; refuse otherwise")
	_ = cmd.MarkFlagRequired("term")
	_ = cmd.RegisterFlagCompletionFunc("term", fixed("12", "24"))
	return cmd
}

func upgradeSummary(machine string, u *wec.UpgradeQuote) string {
	pairs := [][2]string{
		{"VM", ui.Bold.Render(machine)},
		{"Term", planMonths(u.OldTermMonths) + " → " + planMonths(u.TermMonths)},
		{"New term", money(u.NewTotal, "")},
		{"Credit", "-" + money(u.Credit, "") + ui.Dim.Render(fmt.Sprintf("  (%d days left of this term)", u.RemainingDays))},
	}
	var addons, credit float64
	for _, m := range u.AddonMoves {
		addons, credit = addons+m.Total, credit+m.Credit
	}
	if addons > 0 {
		pairs = append(pairs, [2]string{"Add-ons", money(addons, "") + " - " + money(credit, "") +
			ui.Dim.Render("  (its add-on plans move to the new term)")})
	}
	pairs = append(pairs, [2]string{"Total", ui.Bold.Render(money(u.Due, ""))})
	return "\n" + ui.Pairs(pairs) + "\n"
}

// followUpgrade waits for a plan upgrade to finish.
func (r *run) followUpgrade(cmd *cobra.Command, c *wec.Client, upgradeID string) error {
	spin := ui.StartSpinner(r.app.Err, r.tty(), "Upgrading…")
	u, err := c.WaitUpgrade(cmd.Context(), upgradeID)
	spin.Stop()
	if err != nil {
		return err
	}
	if u.Status != "done" {
		detail := u.Status
		if u.ErrorDetail != nil {
			detail = *u.ErrorDetail
		} else if u.ErrorCode != nil {
			detail = *u.ErrorCode
		}
		return fmt.Errorf("the upgrade didn't go through: %s · upgrade %s", detail, u.UpgradeID)
	}
	return r.show(u, []string{u.UpgradeID}, func() string {
		msg := "The plan is now " + planMonths(u.TermMonths)
		if u.NewEndsAt != nil {
			msg += ", to " + day(*u.NewEndsAt)
		}
		return ui.Success(msg) + "\n"
	})
}

func upgradeCmd(r *run) *cobra.Command {
	get := &cobra.Command{
		Use:   "get <upgrade-id>",
		Short: "Show a plan upgrade's progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			u, err := c.GetUpgrade(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return r.show(u, []string{u.UpgradeID}, func() string {
				pairs := [][2]string{
					{"Upgrade", u.UpgradeID},
					{"Status", ui.State(u.Status)},
					{"Term", planMonths(u.OldTermMonths) + " → " + planMonths(u.TermMonths)},
				}
				if u.NetTotal != nil {
					pairs = append(pairs, [2]string{"Charged", money(*u.NetTotal, "")})
				}
				if u.NewEndsAt != nil {
					pairs = append(pairs, [2]string{"Ends", day(*u.NewEndsAt)})
				}
				if u.ErrorDetail != nil {
					pairs = append(pairs, [2]string{"Error", *u.ErrorDetail})
				}
				return ui.Pairs(pairs)
			})
		},
	}
	wait := &cobra.Command{
		Use:   "wait <upgrade-id>",
		Short: "Wait for a plan upgrade to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			return r.followUpgrade(cmd, c, args[0])
		},
	}
	return group("upgrade", "Plan upgrades to a longer term", []string{"upgrades"}, get, wait)
}

func planAddonCmd(r *run) *cobra.Command {
	return group("addon", "A VM plan's add-ons: backups and a public address", []string{"addons"},
		planAddonQuoteCmd(r), planAddonAddCmd(r), planAddonRemoveCmd(r))
}

// addonKind reads an add-on argument: the API's name for it and the CLI's.
func addonKind(arg string) (string, string, error) {
	switch strings.ToLower(arg) {
	case "backups", "backup":
		return "backup", "backups", nil
	case "address", "ip", "ip_address", "public-address":
		return "ip_address", "public address", nil
	}
	return "", "", usagef("%q isn't an add-on: use backups or address", arg)
}

// addonArgs completes a machine name, then the add-on.
func addonArgs(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	machines := machineArg(r)
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return []string{"backups", "address"}, cobra.ShellCompDirectiveNoFileComp
		}
		return machines(cmd, args, toComplete)
	}
}

func planAddonQuoteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "quote <plan|vm>",
		Short:             "What backups and a public address cost on a plan",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			p, machine, err := machinePlan(cmd, c, args[0])
			if err != nil {
				return err
			}
			q, err := c.QuoteAddons(cmd.Context(), p.Ledger.PlanID)
			if err != nil {
				return err
			}
			held := map[string]bool{}
			for _, a := range p.Ledger.Addons {
				held[a.Type] = a.Status == "available" || a.Status == "claimed"
			}
			if ledger, _ := wec.LedgerOf(p); ledger != nil {
				for _, e := range ledger.Entitlements {
					held[e.Type] = held[e.Type] || e.Status == "claimed"
				}
			}
			return r.show(q, nil, func() string {
				renews := "ends"
				if q.PlanRenews {
					renews = "renews"
				}
				head := ui.Bold.Render(machine) + ui.Dim.Render(fmt.Sprintf(" · %d-month plan, %s %s", q.TermMonths,
					renews, day(q.PlanEndsAt))) + "\n\n"
				rows := [][]string{}
				for _, kind := range []string{"backup", "ip_address"} {
					_, label, _ := addonKind(kind)
					on := dash("")
					if held[kind] {
						on = ui.Green.Render("✓") + " on"
					}
					a := q.Price(kind)
					if a == nil {
						rows = append(rows, []string{label, ui.Dim.Render("not sold here"), dash(""), dash(""), dash(""), on})
						continue
					}
					rows = append(rows, []string{label, money(a.Total, ""), money(a.Back, ""), money(a.Net, ""),
						money(a.Monthly, ""), on})
				}
				return head + ui.Table([]string{"ADD-ON", "CHARGED", "BACK NOW", "NET", "A MONTH", "ON"}, rows)
			})
		},
	}
}

func planAddonAddCmd(r *run) *cobra.Command {
	var network, address string
	var maxPrice float64
	cmd := &cobra.Command{
		Use:   "add <plan|vm> backups|address",
		Short: "Add backups or a public address to a plan (charges the wallet)",
		Long: "Add backups or a public address to a running VM's plan, bought for the rest of\n" +
			"the plan's term. gowec prices it first and asks before charging the wallet; in\n" +
			"scripts, pass --yes, or --max-price to refuse anything dearer.",
		Example: "  gowec plan addon add web-1 backups\n" +
			"  gowec plan addon add web-1 address --network Default-Subnet",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: addonArgs(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, label, err := addonKind(args[1])
			if err != nil {
				return err
			}
			if kind == "backup" && (network != "" || address != "") {
				return usagef("--network and --address are for an address")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Pricing…")
			p, machine, err := machinePlan(cmd, c, args[0])
			var q *wec.AddonQuote
			var networkID, addressID string
			if err == nil {
				q, err = c.QuoteAddons(cmd.Context(), p.Ledger.PlanID)
			}
			if err == nil && kind == "ip_address" {
				networkID, addressID, err = addressTarget(cmd, c, p.Ledger.VMID, network, address)
			}
			spin.Stop()
			if err != nil {
				return err
			}
			price := q.Price(kind)
			if price == nil {
				return fmt.Errorf("a %s isn't sold where %s runs", label, machine)
			}

			if r.output == "table" && !r.quiet {
				pairs := [][2]string{
					{"VM", ui.Bold.Render(machine)},
					{"Add-on", label + ui.Dim.Render(fmt.Sprintf(", to the end of the plan's %d-month term", q.TermMonths))},
					{"Charged", ui.Bold.Render(money(price.Total, ""))},
				}
				if price.Back > 0 {
					pairs = append(pairs, [2]string{"Back now", money(price.Back, "") +
						ui.Dim.Render("  (the days past the plan's end; you pay "+money(price.Net, "")+")")})
				}
				fmt.Fprint(r.app.Err, "\n"+ui.Pairs(pairs)+"\n")
			}
			if maxPrice > 0 && price.Total > maxPrice {
				return fmt.Errorf("the price, %s, is above --max-price %s; nothing was charged",
					money(price.Total, ""), money(maxPrice, ""))
			}
			if maxPrice <= 0 {
				question := fmt.Sprintf("Add %s to %s and charge %s to the wallet?", label, machine, money(price.Total, ""))
				if err := r.confirm(question, "adding an add-on charges the wallet"); err != nil {
					return err
				}
			}

			spin = ui.StartSpinner(r.app.Err, r.tty(), "Adding "+label+"…")
			var out map[string]any
			if kind == "backup" {
				out, err = c.AddBackups(cmd.Context(), p.Ledger.PlanID, price.Total, r.idempotencyKey())
			} else {
				out, err = c.AddAddress(cmd.Context(), p.Ledger.PlanID, networkID, addressID, price.Total, r.idempotencyKey())
			}
			spin.Stop()
			if wec.IsCode(err, "plan_price_changed") {
				return priceMoved(err)
			}
			if err != nil {
				return err
			}
			return r.show(out, []string{p.Ledger.PlanID}, func() string {
				msg := "Backups are on for " + machine
				if ip, _ := out["ip_address"].(string); kind == "ip_address" {
					msg = machine + " has the public address " + dash(ip)
				}
				if charged, ok := out["charged"].(float64); ok {
					msg += " · charged " + money(charged, "")
				}
				return ui.Success(msg) + "\n"
			})
		},
	}
	cmd.Flags().StringVar(&network, "network", "", "address only: the VM's network to put it on (default: its default network)")
	cmd.Flags().StringVar(&address, "address", "", "address only: a free address the account holds (IP or id); default: a new one")
	cmd.Flags().Float64Var(&maxPrice, "max-price", 0, "add without asking if the price is at most this; refuse otherwise")
	return cmd
}

// addressTarget is where a new address goes: the machine's network named by
// network (name or id), else its default one; and the id of address, when
// one the account holds was named.
func addressTarget(cmd *cobra.Command, c *wec.Client, vmID, network, address string) (string, string, error) {
	m, err := c.GetMachine(cmd.Context(), vmID)
	if err != nil {
		return "", "", err
	}
	networkID := ""
	for _, a := range m.Addresses {
		if network == "" && (a.IsDefault || len(m.Addresses) == 1) ||
			network != "" && (a.NetworkID == network || strings.EqualFold(a.NetworkName, network)) {
			networkID = a.NetworkID
			break
		}
	}
	if networkID == "" {
		names := make([]string, len(m.Addresses))
		for i, a := range m.Addresses {
			names[i] = a.NetworkName
		}
		return "", "", usagef("%s isn't on a network %s; its networks: %s", m.Name, network, strings.Join(names, ", "))
	}
	if address == "" {
		return networkID, "", nil
	}
	addressID, err := c.ResolveAddressID(cmd.Context(), address)
	return networkID, addressID, err
}

func planAddonRemoveCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "remove <plan|vm> backups|address",
		Aliases:           []string{"rm"},
		Short:             "Remove backups or a public address from a plan (refunds the days left)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: addonArgs(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, label, err := addonKind(args[1])
			if err != nil {
				return err
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			p, machine, err := machinePlan(cmd, c, args[0])
			if err != nil {
				return err
			}
			back := ""
			if ledger, _ := wec.LedgerOf(p); ledger != nil {
				for _, a := range ledger.Addons {
					if a.Type == kind && (a.Status == "available" || a.Status == "claimed") && a.Refund != nil {
						back = fmt.Sprintf(" %s goes back to the wallet.", money(*a.Refund, ""))
					}
				}
			}
			question := fmt.Sprintf("Turn %s's backups off? Its backups are deleted.%s", machine, back)
			if kind == "ip_address" {
				question = fmt.Sprintf("Release %s's public address? It can't be got back.%s", machine, back)
			}
			if err := r.confirm(question, "removing an add-on can't be undone"); err != nil {
				return err
			}
			out, err := c.RemoveAddon(cmd.Context(), p.Ledger.PlanID, kind)
			if err != nil {
				return err
			}
			return r.show(out, []string{p.Ledger.PlanID}, func() string {
				msg := fmt.Sprintf("The %s is off %s", label, machine)
				if kind == "backup" {
					msg = "Backups are off for " + machine
				}
				refunded, _ := out["refunded"].(float64)
				if pending, _ := out["refund_pending"].(bool); pending {
					msg += fmt.Sprintf("; %s goes back to the wallet shortly", money(refunded, ""))
				} else if refunded > 0 {
					msg += fmt.Sprintf("; %s went back to the wallet", money(refunded, ""))
				}
				return ui.Success(msg) + "\n"
			})
		},
	}
}
