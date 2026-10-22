package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"git.wiline.com/cloud/cloud-cli/internal/ui"
	"git.wiline.com/cloud/cloud-cli/pkg/wec"
)

func vpcCmd(r *run) *cobra.Command {
	return group("vpc", "Virtual private clouds", []string{"vpcs"},
		vpcListCmd(r), vpcGetCmd(r), vpcCreateCmd(r), vpcRenameCmd(r), vpcRestartCmd(r),
		vpcSourceNATCmd(r), vpcDeleteCmd(r))
}

func vpcArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("vpc", listVPCNames)
}

func listVPCNames(ctx context.Context, c *wec.Client) ([]string, error) {
	vpcs, err := c.ListVPCs(ctx, wec.ListOptions{})
	names := make([]string, len(vpcs))
	for i, v := range vpcs {
		names[i] = v.Name
	}
	return names, err
}

// vpcState colours a VPC's state; CloudStack calls a working one Enabled.
func vpcState(s string) string {
	if strings.EqualFold(s, "enabled") {
		return ui.Green.Render(s)
	}
	return ui.State(s)
}

// vpcParts are what the VPC views join on: networks and addresses by VPC id.
// Fail-open, so a hiccup leaves those columns blank, not the list.
func vpcParts(ctx context.Context, c *wec.Client) (map[string][]wec.Network, map[string][]wec.Address) {
	networks := map[string][]wec.Network{}
	addresses := map[string][]wec.Address{}
	if all, err := c.ListNetworks(ctx, wec.ListOptions{}); err == nil {
		for _, n := range all {
			networks[n.VPCID] = append(networks[n.VPCID], n)
		}
	}
	if all, err := c.ListAddresses(ctx, wec.ListOptions{}); err == nil {
		for _, a := range all {
			addresses[a.VPCID] = append(addresses[a.VPCID], a)
		}
	}
	return networks, addresses
}

func vpcListCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List VPCs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			vpcs, err := c.ListVPCs(cmd.Context(), wec.ListOptions{})
			if err != nil {
				return err
			}
			ids := make([]string, len(vpcs))
			for i, v := range vpcs {
				ids[i] = v.ID
			}
			if r.output != "table" || r.quiet {
				return r.show(vpcs, ids, nil)
			}
			networks, addresses := vpcParts(cmd.Context(), c)
			rows := make([][]string, len(vpcs))
			for i, v := range vpcs {
				var names []string
				for _, n := range networks[v.ID] {
					names = append(names, n.Name)
				}
				nets := ui.Dim.Render("No networks")
				if len(names) > 0 {
					nets = clip(strings.Join(names, ", "), 40) + "\n" + ui.Dim.Render(countNoun(len(names), "network"))
				}
				source := ui.Dim.Render("—")
				for _, a := range addresses[v.ID] {
					if a.IsSourceNAT {
						source = a.IPAddress
					}
				}
				rows[i] = []string{ui.Bold.Render(v.Name) + "\n" + ui.Dim.Render(dash(v.CIDR)), vpcState(v.State),
					nets, source, zoneName(v.ZoneName), ui.Dim.Render(v.ID)}
			}
			return r.show(vpcs, ids, func() string {
				if len(vpcs) == 0 {
					return ui.Dim.Render("No VPCs.") + "\n"
				}
				return ui.RowTable([]string{"VPC", "STATE", "NETWORKS", "SOURCE NAT", "ZONE", "ID"}, rows)
			})
		},
	}
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func vpcGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <vpc>",
		Aliases:           []string{"show", "describe"},
		Short:             "Show one VPC with its networks and addresses (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if r.output != "table" || r.quiet {
				return r.show(v, []string{v.ID}, nil)
			}
			networks, addresses := vpcParts(cmd.Context(), c)
			pairs := [][2]string{
				{"Name", ui.Bold.Render(v.Name)},
				{"State", vpcState(v.State)},
				{"ID", ui.Dim.Render(v.ID)},
				{"CIDR", dash(v.CIDR)},
				{"Zone", zoneName(v.ZoneName)},
			}
			for _, n := range networks[v.ID] {
				pairs = append(pairs, [2]string{"Network", n.Name + "  " + ui.Dim.Render(n.CIDR)})
			}
			for _, a := range addresses[v.ID] {
				role := ""
				switch {
				case a.IsSourceNAT:
					role = "source NAT"
				case a.StaticNATMachineName != "":
					role = "→ " + a.StaticNATMachineName
				}
				pairs = append(pairs, [2]string{"Address", a.IPAddress + "  " + ui.Dim.Render(role)})
			}
			return r.show(v, []string{v.ID}, func() string { return ui.Pairs(pairs) })
		},
	}
}

func vpcCreateCmd(r *run) *cobra.Command {
	var body wec.CreateVPC
	var zone string
	cmd := &cobra.Command{
		Use:     "create --name <name> --zone <zone> --cidr <cidr> --offering <id>",
		Aliases: []string{"new"},
		Short:   "Create a VPC",
		Example: "  gowec vpc create --name prod --zone princeton --cidr 10.1.0.0/16 --offering <vpc-offering-id>",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if body.ZoneID, err = c.ResolveZoneID(cmd.Context(), zone); err != nil {
				return err
			}
			accepted, err := c.CreateVPC(cmd.Context(), body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Creating "+body.Name, "Created "+body.Name)
		},
	}
	f := cmd.Flags()
	f.StringVar(&body.Name, "name", "", "the VPC's name")
	f.StringVar(&zone, "zone", "", "zone name or id (gowec zone list)")
	f.StringVar(&body.CIDR, "cidr", "", "the VPC's address range, e.g. 10.1.0.0/16")
	f.StringVar(&body.VPCOfferingID, "offering", "", "the VPC offering's id")
	f.StringVar(&body.DisplayText, "description", "", "a description")
	for _, name := range []string{"name", "zone", "offering"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func vpcRenameCmd(r *run) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "rename <vpc> --name <new-name>",
		Aliases:           []string{"update"},
		Short:             "Rename a VPC",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.RenameVPC(cmd.Context(), v.ID, name)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Renaming "+v.Name, v.Name+" is now "+name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the new name")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func vpcRestartCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "restart <vpc>",
		Short:             "Restart a VPC's router",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.RestartVPC(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Restarting "+v.Name, v.Name+" restarted")
		},
	}
}

func vpcSourceNATCmd(r *run) *cobra.Command {
	var address string
	cmd := &cobra.Command{
		Use:               "source-nat <vpc> --address <ip>",
		Short:             "Change the address a VPC's outbound traffic leaves from",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			a, err := c.FindAddress(cmd.Context(), address)
			if err != nil {
				return err
			}
			accepted, err := c.SetVPCSourceNAT(cmd.Context(), v.ID, a.IPAddress)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Moving "+v.Name+"'s source NAT",
				v.Name+"'s traffic now leaves from "+a.IPAddress)
		},
	}
	cmd.Flags().StringVar(&address, "address", "", "one of the VPC's public addresses (IP or id)")
	_ = cmd.MarkFlagRequired("address")
	netCompleteFlag(r, cmd, "address", "address", listAddressIPs)
	return cmd
}

func vpcDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <vpc>",
		Aliases:           []string{"rm"},
		Short:             "Delete a VPC and release its addresses",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, err := c.FindVPC(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Delete the VPC %s? Its public addresses are released.", v.Name),
				"deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteVPC(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Deleting "+v.Name, v.Name+" is deleted")
		},
	}
}
