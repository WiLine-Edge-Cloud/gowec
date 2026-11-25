package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

const listenerHelp = `A listener is PUBLIC:PRIVATE, then optional key=value settings, e.g.
  80:8080
  443:8443,algorithm=leastconn,sticky=source
  25:25,protocol=tcp-proxy
algorithm: roundrobin (default), leastconn or source
sticky:    none (default), source, or cookie (only on public port 80,
           which is always HTTP)
protocol:  tcp (default), or tcp-proxy to send the PROXY protocol header`

func lbCmd(r *run) *cobra.Command {
	return group("lb", "Load balancers: one public address spread over a subnet's machines",
		[]string{"lbs", "load-balancer", "load-balancers"},
		lbListCmd(r), lbGetCmd(r), lbCreateCmd(r), lbDeleteCmd(r), lbMachinesCmd(r), lbListenersCmd(r))
}

func lbMachinesCmd(r *run) *cobra.Command {
	return group("machines", "The machines behind a load balancer", []string{"machine", "vms", "vm"},
		lbMachinesAddCmd(r), lbMachinesRemoveCmd(r))
}

func lbListenersCmd(r *run) *cobra.Command {
	return group("listeners", "A load balancer's listeners: the public ports it passes on", []string{"listener"},
		lbListenerAddCmd(r), lbListenerUpdateCmd(r), lbListenerRemoveCmd(r))
}

// lbTarget finds the VPC and one of its load balancers (name, address or id).
func lbTarget(ctx context.Context, c *wec.Client, vpc, ref string) (*wec.VPC, *wec.LoadBalancer, error) {
	v, err := c.FindVPC(ctx, vpc)
	if err != nil {
		return nil, nil, err
	}
	lb, err := c.FindLoadBalancer(ctx, v.ID, ref)
	return v, lb, err
}

func addVPCFlag(r *run, cmd *cobra.Command, vpc *string) {
	cmd.Flags().StringVar(vpc, "vpc", "", "the VPC (name or id)")
	_ = cmd.MarkFlagRequired("vpc")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
}

// parseListener reads PUBLIC:PRIVATE[,key=value...] (see listenerHelp).
func parseListener(s string) (wec.ListenerSpec, error) {
	var spec wec.ListenerSpec
	parts := strings.Split(s, ",")
	ports := strings.SplitN(parts[0], ":", 2)
	if len(ports) != 2 {
		return spec, usagef("listener %q: write it as PUBLIC:PRIVATE, e.g. 80:8080", s)
	}
	var err error
	if spec.PublicPort, err = strconv.Atoi(strings.TrimSpace(ports[0])); err != nil {
		return spec, usagef("listener %q: the public port must be a number", s)
	}
	if spec.PrivatePort, err = strconv.Atoi(strings.TrimSpace(ports[1])); err != nil {
		return spec, usagef("listener %q: the private port must be a number", s)
	}
	for _, kv := range parts[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(kv), "=")
		switch {
		case !ok:
			return spec, usagef("listener %q: %q isn't key=value", s, kv)
		case key == "algorithm":
			spec.Algorithm = value
		case key == "sticky":
			spec.Sticky = value
		case key == "protocol":
			spec.Protocol = value
		default:
			return spec, usagef("listener %q: unknown setting %q (algorithm, sticky or protocol)", s, key)
		}
	}
	return spec, nil
}

// machineIDs turns VM names or ids into ids.
func machineIDs(ctx context.Context, c *wec.Client, refs []string) ([]string, []string, error) {
	ids, names := make([]string, len(refs)), make([]string, len(refs))
	for i, ref := range refs {
		m, err := c.GetMachine(ctx, ref)
		if err != nil {
			return nil, nil, err
		}
		ids[i], names[i] = m.ID, m.Name
	}
	return ids, names, nil
}

// findListener finds a balancer's listener by public port or id.
func findListener(lb *wec.LoadBalancer, ref string) (*wec.LoadBalancerListener, error) {
	for i, l := range lb.Listeners {
		if strconv.Itoa(l.PublicPort) == ref || strings.EqualFold(l.ID, ref) {
			return &lb.Listeners[i], nil
		}
	}
	return nil, &wec.APIError{Status: 404, Code: "not_found",
		Message: fmt.Sprintf("%s has no listener on port %s", lb.Name, ref)}
}

func listenerText(l wec.LoadBalancerListener) string {
	return fmt.Sprintf("%d → %d", l.PublicPort, l.PrivatePort)
}

func lbState(s string) string {
	if s == "ready" {
		return ui.Green.Render(s)
	}
	return ui.Yellow.Render(s)
}

func lbListCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "list --vpc <vpc>",
		Aliases: []string{"ls"},
		Short:   "List a VPC's load balancers",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), vpc)
			if err != nil {
				return err
			}
			lbs, err := c.ListLoadBalancers(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			ids := make([]string, len(lbs))
			for i, lb := range lbs {
				ids[i] = lb.ID
			}
			return r.show(lbs, ids, func() string {
				if len(lbs) == 0 {
					return ui.Dim.Render(v.Name+" has no load balancers.") + "\n" +
						ui.Dim.Render(r.app.Refs("  create one with: gowec lb create <name> --vpc "+v.Name+" --subnet <subnet> --listener 80:80")) + "\n"
				}
				rows := make([][]string, len(lbs))
				for i, lb := range lbs {
					ports := make([]string, len(lb.Listeners))
					for j, l := range lb.Listeners {
						ports[j] = listenerText(l)
					}
					running := 0
					for _, m := range lb.Machines {
						if m.State == "Running" {
							running++
						}
					}
					rows[i] = []string{ui.Bold.Render(dash(lb.Name)), lb.Address, strings.Join(ports, "\n"),
						fmt.Sprintf("%d of %d running", running, len(lb.Machines)), lbState(lb.State), ui.Dim.Render(lb.ID)}
				}
				return ui.RowTable([]string{"NAME", "ADDRESS", "LISTENERS", "MACHINES", "STATE", "ID"}, rows)
			})
		},
	}
	addVPCFlag(r, cmd, &vpc)
	return cmd
}

func lbGetCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "get <lb> --vpc <vpc>",
		Aliases: []string{"show"},
		Short:   "Show a load balancer: its address, listeners and machines",
		Long:    "Show a load balancer (name, address or id). A machine's state is the machine's\nown; load balancers don't report health yet.",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			_, lb, err := lbTarget(cmd.Context(), c, vpc, args[0])
			if err != nil {
				return err
			}
			return r.show(lb, []string{lb.ID}, func() string { return lbTable(lb) })
		},
	}
	addVPCFlag(r, cmd, &vpc)
	return cmd
}

func lbTable(lb *wec.LoadBalancer) string {
	out := ui.Pairs([][2]string{
		{"Name", ui.Bold.Render(dash(lb.Name))},
		{"Address", lb.Address},
		{"Subnet", dash(strings.TrimSpace(lb.Subnet.Name + "  " + ui.Dim.Render(lb.Subnet.CIDR)))},
		{"State", lbState(lb.State)},
	})
	listeners := make([][]string, len(lb.Listeners))
	for i, l := range lb.Listeners {
		listeners[i] = []string{listenerText(l), l.Protocol, dash(l.Algorithm), dash(l.Sticky), ui.Dim.Render(l.ID)}
	}
	out += "\n" + ui.RowTable([]string{"LISTENER", "PROTOCOL", "ALGORITHM", "STICKY", "ID"}, listeners)
	if len(lb.Machines) == 0 {
		return out + "\n" + ui.Dim.Render("No machines behind it yet: gowec lb machines add "+lb.Name+" --vpc <vpc> --vm <vm>") + "\n"
	}
	machines := make([][]string, len(lb.Machines))
	for i, m := range lb.Machines {
		machines[i] = []string{ui.Bold.Render(dash(m.Name)), dash(m.PrivateAddress), ui.State(m.State), ui.Dim.Render(m.ID)}
	}
	return out + "\n" + ui.RowTable([]string{"MACHINE", "ADDRESS", "STATE", "ID"}, machines)
}

func lbCreateCmd(r *run) *cobra.Command {
	var vpc, subnet string
	var listeners, vms []string
	cmd := &cobra.Command{
		Use:     "create <name> --vpc <vpc> --subnet <subnet> --listener <spec>... [--vm <vm>...]",
		Aliases: []string{"new"},
		Short:   "Create a load balancer on a new public address",
		Long: "Create a load balancer on a new public address of the VPC. It sends each\n" +
			"listener's public port to the machines on one subnet. On postpaid the address\n" +
			"is billed like any other; on prepaid it is free for now. gowec asks first, and\n" +
			"in scripts you pass --yes.\n\n" +
			listenerHelp,
		Example: "  gowec lb create web --vpc prod --subnet front --listener 80:8080 --listener 443:8443 --vm web-1,web-2",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := wec.CreateLoadBalancer{Name: args[0], MachineIDs: []string{}}
			for _, s := range listeners {
				spec, err := parseListener(s)
				if err != nil {
					return err
				}
				body.Listeners = append(body.Listeners, spec)
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := c.FindVPC(ctx, vpc)
			if err != nil {
				return err
			}
			n, err := c.FindSubnet(ctx, v.ID, subnet)
			if err != nil {
				return err
			}
			body.SubnetID = n.ID
			if len(vms) > 0 {
				if body.MachineIDs, _, err = machineIDs(ctx, c, vms); err != nil {
					return err
				}
			}
			question := fmt.Sprintf("Create load balancer %s on %s? It reserves a new public address.", body.Name, n.Name)
			if err := r.confirm(question, "creating a load balancer reserves an address"); err != nil {
				return err
			}
			accepted, err := c.CreateLoadBalancer(ctx, v.ID, body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			op := &accepted.Operation
			if r.detach && op.OperationID != "" {
				return r.show(accepted, []string{accepted.LoadBalancerID}, func() string {
					return ui.Success("Creating "+body.Name+" · operation "+op.OperationID) + "\n"
				})
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Creating "+body.Name+"…")
			op, err = c.WaitOperation(ctx, op, nil)
			spin.Stop()
			if err != nil {
				return err
			}
			if op.Status == "failed" {
				return fmt.Errorf("%s", dash(op.ErrorDetail))
			}
			lb, err := c.GetLoadBalancer(ctx, v.ID, accepted.LoadBalancerID)
			if err != nil {
				return r.show(op, []string{accepted.LoadBalancerID}, func() string { return ui.Success("Created "+body.Name) + "\n" })
			}
			return r.show(lb, []string{lb.ID}, func() string {
				return ui.Success("Created "+lb.Name+" on "+lb.Address) + "\n\n" + lbTable(lb)
			})
		},
	}
	f := cmd.Flags()
	addVPCFlag(r, cmd, &vpc)
	f.StringVar(&subnet, "subnet", "", "the VPC subnet its machines are on (name or id)")
	f.StringArrayVar(&listeners, "listener", nil, "a listener, PUBLIC:PRIVATE[,key=value…]; repeat for more (1 to 10)")
	f.StringSliceVar(&vms, "vm", nil, "machines to put behind it, comma-separated (name or id)")
	_ = cmd.MarkFlagRequired("subnet")
	_ = cmd.MarkFlagRequired("listener")
	netCompleteFlag(r, cmd, "subnet", "network", listNetworkNames)
	netCompleteFlag(r, cmd, "vm", "vm", listMachineNames)
	return cmd
}

func lbDeleteCmd(r *run) *cobra.Command {
	var vpc string
	var release bool
	cmd := &cobra.Command{
		Use:     "delete <lb> --vpc <vpc> [--release-address]",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete a load balancer (its address is kept unless --release-address)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, lb, err := lbTarget(cmd.Context(), c, vpc, args[0])
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Delete %s? Visitors to %s stop reaching your machines; the address stays yours.", lb.Name, lb.Address)
			if release {
				question = fmt.Sprintf("Delete %s and release %s? You may not get the same address again.", lb.Name, lb.Address)
			}
			if err := r.confirm(question, "deleting a load balancer"); err != nil {
				return err
			}
			op, err := c.DeleteLoadBalancer(cmd.Context(), v.ID, lb.ID, release)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitOperation(cmd.Context(), c, op, "Deleting "+lb.Name, "Deleted "+lb.Name)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	cmd.Flags().BoolVar(&release, "release-address", false, "also give the public address back")
	return cmd
}

func lbMachinesAddCmd(r *run) *cobra.Command {
	var vpc string
	var vms []string
	cmd := &cobra.Command{
		Use:   "add <lb> --vpc <vpc> --vm <vm>[,<vm>…]",
		Short: "Put machines from the balancer's subnet behind every listener",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, lb, err := lbTarget(ctx, c, vpc, args[0])
			if err != nil {
				return err
			}
			ids, names, err := machineIDs(ctx, c, vms)
			if err != nil {
				return err
			}
			op, err := c.AddLoadBalancerMachines(ctx, v.ID, lb.ID, ids, r.idempotencyKey())
			if err != nil {
				return err
			}
			list := strings.Join(names, ", ")
			return r.waitOperation(ctx, c, op, "Adding "+list+" to "+lb.Name, "Added "+list+" to "+lb.Name)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	cmd.Flags().StringSliceVar(&vms, "vm", nil, "machines to add, comma-separated (name or id)")
	_ = cmd.MarkFlagRequired("vm")
	netCompleteFlag(r, cmd, "vm", "vm", listMachineNames)
	return cmd
}

func lbMachinesRemoveCmd(r *run) *cobra.Command {
	var vpc, vm string
	cmd := &cobra.Command{
		Use:     "remove <lb> --vpc <vpc> --vm <vm>",
		Aliases: []string{"rm"},
		Short:   "Take a machine out of a load balancer (the machine itself is not changed)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, lb, err := lbTarget(ctx, c, vpc, args[0])
			if err != nil {
				return err
			}
			ids, names, err := machineIDs(ctx, c, []string{vm})
			if err != nil {
				return err
			}
			question := fmt.Sprintf("Take %s out of %s? %s stops sending it connections; the machine stays as it is.", names[0], lb.Name, lb.Name)
			if err := r.confirm(question, "removing a machine from a load balancer"); err != nil {
				return err
			}
			op, err := c.RemoveLoadBalancerMachine(ctx, v.ID, lb.ID, ids[0])
			if err != nil {
				return err
			}
			return r.waitOperation(ctx, c, op, "Taking "+names[0]+" out of "+lb.Name, "Took "+names[0]+" out of "+lb.Name)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	cmd.Flags().StringVar(&vm, "vm", "", "the machine to take out (name or id)")
	_ = cmd.MarkFlagRequired("vm")
	netCompleteFlag(r, cmd, "vm", "vm", listMachineNames)
	return cmd
}

func lbListenerAddCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "add <lb> <spec> --vpc <vpc>",
		Short:   "Add a listener; the balancer's machines join it",
		Long:    "Add a listener to a load balancer (up to 10). Its machines join it.\n\n" + listenerHelp,
		Example: "  gowec lb listeners add web 443:8443,algorithm=leastconn --vpc prod",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := parseListener(args[1])
			if err != nil {
				return err
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, lb, err := lbTarget(ctx, c, vpc, args[0])
			if err != nil {
				return err
			}
			op, err := c.AddListener(ctx, v.ID, lb.ID, spec, r.idempotencyKey())
			if err != nil {
				return err
			}
			port := strconv.Itoa(spec.PublicPort)
			return r.waitOperation(ctx, c, op, "Adding port "+port+" to "+lb.Name, lb.Name+" listens on port "+port)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	return cmd
}

func lbListenerUpdateCmd(r *run) *cobra.Command {
	var vpc string
	var changes wec.ListenerChanges
	cmd := &cobra.Command{
		Use:   "update <lb> <port|id> --vpc <vpc> [--algorithm <a>] [--sticky <s>]",
		Short: "Change a listener's algorithm or stickiness (ports can't change)",
		Long: "Change how a listener spreads connections. Its ports can't change: remove it and\n" +
			"add another.\n\nalgorithm: roundrobin, leastconn or source\n" +
			"sticky:    none, source, or cookie (only on public port 80)",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if changes.Algorithm == "" && changes.Sticky == "" {
				return usagef("pass --algorithm, --sticky or both")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, lb, err := lbTarget(ctx, c, vpc, args[0])
			if err != nil {
				return err
			}
			l, err := findListener(lb, args[1])
			if err != nil {
				return err
			}
			op, err := c.UpdateListener(ctx, v.ID, lb.ID, l.ID, changes)
			if err != nil {
				return err
			}
			port := strconv.Itoa(l.PublicPort)
			return r.waitOperation(ctx, c, op, "Changing port "+port+" on "+lb.Name, "Changed port "+port+" on "+lb.Name)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	cmd.Flags().StringVar(&changes.Algorithm, "algorithm", "", "roundrobin, leastconn or source")
	cmd.Flags().StringVar(&changes.Sticky, "sticky", "", "none, source or cookie")
	_ = cmd.RegisterFlagCompletionFunc("algorithm", fixed("roundrobin", "leastconn", "source"))
	_ = cmd.RegisterFlagCompletionFunc("sticky", fixed("none", "source", "cookie"))
	return cmd
}

func lbListenerRemoveCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "remove <lb> <port|id> --vpc <vpc>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a listener (not the last one: delete the balancer instead)",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, lb, err := lbTarget(ctx, c, vpc, args[0])
			if err != nil {
				return err
			}
			l, err := findListener(lb, args[1])
			if err != nil {
				return err
			}
			port := strconv.Itoa(l.PublicPort)
			question := fmt.Sprintf("Remove the listener on port %s? Visitors to %s on port %s stop reaching your machines.", port, lb.Address, port)
			if err := r.confirm(question, "removing a listener"); err != nil {
				return err
			}
			op, err := c.DeleteListener(ctx, v.ID, lb.ID, l.ID)
			if err != nil {
				return err
			}
			return r.waitOperation(ctx, c, op, "Removing port "+port+" from "+lb.Name, "Removed port "+port+" from "+lb.Name)
		},
	}
	addVPCFlag(r, cmd, &vpc)
	return cmd
}
