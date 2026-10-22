package cli

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func networkCmd(r *run) *cobra.Command {
	return group("network", "Networks (subnets)", []string{"networks", "net", "subnet", "subnets"},
		networkListCmd(r), networkGetCmd(r), networkCreateCmd(r), networkRenameCmd(r),
		networkRestartCmd(r), networkDeleteCmd(r), networkRulesCmd(r))
}

func networkArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("network", listNetworkNames)
}

func listNetworkNames(ctx context.Context, c *wec.Client) ([]string, error) {
	networks, err := c.ListNetworks(ctx, wec.ListOptions{})
	names := make([]string, len(networks))
	for i, n := range networks {
		names[i] = n.Name
	}
	return names, err
}

// netCompleteFlag completes a flag's value with names, like a first argument.
func netCompleteFlag(r *run, cmd *cobra.Command, flag, kind string, list NameLister) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return r.app.cachedNames(r, kind, list), cobra.ShellCompDirectiveNoFileComp
	})
}

// waitNet is waitJob for changes CloudStack may finish at once: with no job
// to follow, the change is already done.
func (r *run) waitNet(ctx context.Context, c *wec.Client, accepted *wec.JobAccepted, doing, done string) error {
	if accepted.JobID == "" {
		return r.show(accepted, []string{accepted.ResourceID}, func() string { return ui.Success(done) + "\n" })
	}
	return r.waitJob(ctx, c, accepted, doing, done)
}

func networkListCmd(r *run) *cobra.Command {
	var vpc string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List networks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			networks, err := c.ListNetworks(cmd.Context(), wec.ListOptions{})
			if err != nil {
				return err
			}
			if vpc != "" {
				v, err := c.FindVPC(cmd.Context(), vpc)
				if err != nil {
					return err
				}
				kept := networks[:0]
				for _, n := range networks {
					if n.VPCID == v.ID {
						kept = append(kept, n)
					}
				}
				networks = kept
			}
			ids := make([]string, len(networks))
			rows := make([][]string, len(networks))
			for i, n := range networks {
				ids[i] = n.ID
				rows[i] = []string{ui.Bold.Render(n.Name) + "\n" + ui.Dim.Render(dash(n.CIDR)),
					ui.State(n.State), dash(n.VPCName), zoneName(n.ZoneName), ui.Dim.Render(n.ID)}
			}
			return r.show(networks, ids, func() string {
				if len(networks) == 0 {
					return ui.Dim.Render("No networks.") + "\n"
				}
				return ui.RowTable([]string{"NETWORK", "STATE", "VPC", "ZONE", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&vpc, "vpc", "", "only this VPC's networks (name or id)")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
	return cmd
}

func networkGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <network>",
		Aliases:           []string{"show", "describe"},
		Short:             "Show one network (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: networkArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			n, err := c.FindNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if r.output != "table" || r.quiet {
				return r.show(n, []string{n.ID}, nil)
			}
			pairs := [][2]string{
				{"Name", ui.Bold.Render(n.Name)},
				{"State", ui.State(n.State)},
				{"ID", ui.Dim.Render(n.ID)},
				{"CIDR", dash(n.CIDR)},
				{"VPC", dash(n.VPCName)},
				{"Zone", zoneName(n.ZoneName)},
			}
			// The machines on it, from their interfaces; fail-open.
			if machines, err := c.ListMachines(cmd.Context(), wec.ListOptions{}); err == nil {
				for _, m := range machines {
					for _, a := range m.Addresses {
						if a.NetworkID == n.ID {
							pairs = append(pairs, [2]string{"VM", m.Name + "  " + ui.Dim.Render(a.IPAddress)})
						}
					}
				}
			}
			return r.show(n, []string{n.ID}, func() string { return ui.Pairs(pairs) })
		},
	}
}

func networkCreateCmd(r *run) *cobra.Command {
	var body wec.CreateNetwork
	var vpc, zone, cidr, acl string
	cmd := &cobra.Command{
		Use:     "create --name <name> --vpc <vpc> --cidr <cidr> --offering <id>",
		Aliases: []string{"new"},
		Short:   "Create a network, as a tier of a VPC or on its own",
		Example: "  gowec network create --name web --vpc prod --cidr 10.1.1.0/24 --acl default_allow \\\n" +
			"    --offering <network-offering-id>",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if vpc != "" {
				v, err := c.FindVPC(ctx, vpc)
				if err != nil {
					return err
				}
				body.VPCID, body.ZoneID = v.ID, v.ZoneID
			}
			if zone != "" {
				if body.ZoneID, err = c.ResolveZoneID(ctx, zone); err != nil {
					return err
				}
			}
			if body.ZoneID == "" {
				return usagef("give --zone, or --vpc to use the VPC's zone")
			}
			if cidr != "" {
				if body.Gateway, body.Netmask, err = cidrGateway(cidr); err != nil {
					return err
				}
			}
			if acl != "" {
				a, err := c.FindNetworkACL(ctx, acl)
				if err != nil {
					return err
				}
				body.ACLID = a.ID
			}
			accepted, err := c.CreateNetwork(ctx, body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(ctx, c, accepted, "Creating "+body.Name, "Created "+body.Name)
		},
	}
	f := cmd.Flags()
	f.StringVar(&body.Name, "name", "", "the network's name")
	f.StringVar(&vpc, "vpc", "", "create it as a tier of this VPC (name or id)")
	f.StringVar(&zone, "zone", "", "zone name or id (defaults to the VPC's)")
	f.StringVar(&cidr, "cidr", "", "the tier's range inside the VPC's, e.g. 10.1.1.0/24; its first address is the gateway")
	f.StringVar(&acl, "acl", "", "the network ACL it uses (name or id, gowec firewall acl list)")
	f.StringVar(&body.NetworkOfferingID, "offering", "", "the network offering's id")
	f.StringVar(&body.DisplayText, "description", "", "a description")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("offering")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
	netCompleteFlag(r, cmd, "acl", "acl", listACLNames)
	return cmd
}

// cidrGateway gives a tier's gateway (its first address) and netmask.
func cidrGateway(cidr string) (gateway, netmask string, err error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return "", "", usagef("--cidr %q isn't an IPv4 range like 10.1.1.0/24", cidr)
	}
	mask := net.CIDRMask(prefix.Bits(), 32)
	return prefix.Masked().Addr().Next().String(), net.IP(mask).String(), nil
}

func networkRenameCmd(r *run) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "rename <network> --name <new-name>",
		Aliases:           []string{"update"},
		Short:             "Rename a network",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: networkArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			n, err := c.FindNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.RenameNetwork(cmd.Context(), n.ID, name)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Renaming "+n.Name, n.Name+" is now "+name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the new name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func networkRestartCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "restart <network>",
		Short:             "Restart a network (its router applies its settings again)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: networkArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			n, err := c.FindNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.RestartNetwork(cmd.Context(), n.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Restarting "+n.Name, n.Name+" restarted")
		},
	}
}

func networkDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <network>",
		Aliases:           []string{"rm"},
		Short:             "Delete a network (no VM may be on it)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: networkArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			n, err := c.FindNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Delete the network %s (%s)?", n.Name, dash(n.CIDR)),
				"deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteNetwork(cmd.Context(), n.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Deleting "+n.Name, n.Name+" is deleted")
		},
	}
}

func networkRulesCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "rules <network>",
		Aliases:           []string{"firewall-rules"},
		Short:             "List a network's firewall rules",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: networkArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			n, err := c.FindNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rules, err := c.ListNetworkFirewallRules(cmd.Context(), n.ID)
			if err != nil {
				return err
			}
			ids := make([]string, len(rules))
			rows := make([][]string, len(rules))
			for i, f := range rules {
				ids[i] = f.ID
				rows[i] = []string{dash(f.IPAddress), ruleProtocol(f.Protocol),
					ruleTarget(f.Protocol, f.StartPort, f.EndPort, f.ICMPType, f.ICMPCode),
					ruleCIDRs(f.CIDR), ui.State(f.State), ui.Dim.Render(f.ID)}
			}
			return r.show(rules, ids, func() string {
				if len(rules) == 0 {
					return ui.Dim.Render("No firewall rules on "+n.Name+".") + "\n"
				}
				return ui.Table([]string{"ADDRESS", "PROTOCOL", "PORTS", "CIDR", "STATE", "ID"}, rows)
			})
		},
	}
}

// ruleProtocol is a rule's protocol, "all" when it has none.
func ruleProtocol(p string) string {
	if p == "" {
		return "all"
	}
	return strings.ToLower(p)
}

// ruleTarget is what a rule matches: its ports, or its ICMP type and code.
func ruleTarget(proto string, start, end, icmpType, icmpCode *int) string {
	if strings.EqualFold(proto, "icmp") {
		if icmpType == nil || *icmpType == -1 {
			return ui.Dim.Render("any type")
		}
		label := fmt.Sprintf("type %d", *icmpType)
		if icmpCode != nil && *icmpCode != -1 {
			label += fmt.Sprintf(" code %d", *icmpCode)
		}
		return label
	}
	switch {
	case start == nil && end == nil, strings.EqualFold(proto, "all"):
		return ui.Dim.Render("all")
	case end == nil || start == nil || *start == *end:
		if start == nil {
			start = end
		}
		return strconv.Itoa(*start)
	}
	return fmt.Sprintf("%d-%d", *start, *end)
}

// ruleCIDRs is a rule's comma-separated CIDRs, "any" when it has none.
func ruleCIDRs(list string) string {
	if strings.TrimSpace(list) == "" {
		return ui.Dim.Render("any")
	}
	return strings.ReplaceAll(list, ",", ", ")
}

// parsePortFlag reads --port: one port (22) or a range (8000-8100).
func parsePortFlag(s string) (start, end *int, err error) {
	from, to, isRange := strings.Cut(s, "-")
	a, errA := strconv.Atoi(strings.TrimSpace(from))
	b, errB := a, error(nil)
	if isRange {
		b, errB = strconv.Atoi(strings.TrimSpace(to))
	}
	if errA != nil || errB != nil || a < 0 || b > 65535 || a > b {
		return nil, nil, usagef("--port %q isn't a port or a range like 8000-8100", s)
	}
	return &a, &b, nil
}
