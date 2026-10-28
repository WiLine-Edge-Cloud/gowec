package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func addressCmd(r *run) *cobra.Command {
	return group("address", "Public IP addresses", []string{"addresses", "ip"},
		listCmd(r, "List public addresses",
			func(cmd *cobra.Command, c *wec.Client) ([]wec.Address, error) {
				return c.ListAddresses(cmd.Context(), wec.ListOptions{})
			},
			[]string{"ADDRESS", "STATE", "VPC", "VM", "ZONE", "ID"},
			func(a wec.Address) []string {
				machine := a.StaticNATMachineName
				if a.IsSourceNAT {
					machine = ui.Dim.Render("source NAT")
				}
				return []string{a.IPAddress, ui.State(a.State), dash(a.VPCName), dash(machine),
					zoneName(a.ZoneName), ui.Dim.Render(a.ID)}
			},
			func(a wec.Address) string { return a.ID }),
		addressAllocateCmd(r), addressReleaseCmd(r), addressAttachCmd(r), addressDetachCmd(r))
}

func addressArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("address", listAddressIPs)
}

func listAddressIPs(ctx context.Context, c *wec.Client) ([]string, error) {
	addresses, err := c.ListAddresses(ctx, wec.ListOptions{})
	ips := make([]string, len(addresses))
	for i, a := range addresses {
		ips[i] = a.IPAddress
	}
	return ips, err
}

func addressAllocateCmd(r *run) *cobra.Command {
	var vpc, machine string
	cmd := &cobra.Command{
		Use:     "allocate --vpc <vpc> [--vm <vm>]",
		Aliases: []string{"new", "create"},
		Short:   "Take a new public address in a VPC (it may charge the wallet)",
		Long: "Take a new public address in a VPC. On prepaid, a month of it is charged to the\n" +
			"wallet unless the VM's plan already includes one; an address must be for a\n" +
			"VM. gowec prices it first and asks; in scripts, pass --yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := c.FindVPC(ctx, vpc)
			if err != nil {
				return err
			}
			var machineID, forName string
			if machine != "" {
				m, err := c.GetMachine(ctx, machine)
				if err != nil {
					return err
				}
				machineID, forName = m.ID, " for "+m.Name
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Pricing…")
			quote, err := c.QuoteAddress(ctx, v.ID, machineID)
			spin.Stop()
			if err != nil {
				r.planAddonHint(err, machine, "address", "public address is")
				return err
			}
			question := fmt.Sprintf("Allocate a public address in %s%s?%s", v.Name, forName, chargeNote(quote))
			if err := r.confirm(question, "allocating can charge the wallet"); err != nil {
				return err
			}
			accepted, err := c.AllocateAddress(ctx, v.ID, machineID, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			if r.detach || accepted.JobID == "" {
				return r.waitNet(ctx, c, accepted, "Allocating an address in "+v.Name, "Allocated an address in "+v.Name)
			}
			// Followed here rather than with waitJob, to show the new IP.
			spin = ui.StartSpinner(r.app.Err, r.tty(), "Allocating an address in "+v.Name+"…")
			job, err := c.WaitJob(ctx, accepted.JobID, nil)
			spin.Stop()
			if err != nil {
				return err
			}
			if job.Status == "failed" {
				return fmt.Errorf("allocating an address in %s failed; the account's events have the details · job %s", v.Name, job.JobID)
			}
			a, err := c.FindAddress(ctx, job.ResourceID)
			if err != nil {
				return r.show(job, []string{job.ResourceID}, func() string { return ui.Success("Allocated an address in "+v.Name) + "\n" })
			}
			return r.show(a, []string{a.ID}, func() string {
				out := ui.Success("Allocated "+a.IPAddress+" in "+v.Name) + "\n"
				if machine != "" {
					out += ui.Dim.Render(r.app.Refs("  point it at the VM with: gowec address attach "+a.IPAddress+" --vm "+machine)) + "\n"
				}
				return out
			})
		},
	}
	cmd.Flags().StringVar(&vpc, "vpc", "", "the VPC (name or id)")
	cmd.Flags().StringVar(&machine, "vm", "", "the VM it is for (name or id); its plan may pay for it")
	_ = cmd.MarkFlagRequired("vpc")
	netCompleteFlag(r, cmd, "vpc", "vpc", listVPCNames)
	netCompleteFlag(r, cmd, "vm", "vm", listMachineNames)
	return cmd
}

func addressReleaseCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "release <ip>",
		Aliases:           []string{"delete", "rm"},
		Short:             "Give a public address back (by IP or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: addressArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindAddress(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Release %s? You may not get the same address again.", a.IPAddress),
				"releasing can't be undone"); err != nil {
				return err
			}
			accepted, err := c.ReleaseAddress(cmd.Context(), a.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Releasing "+a.IPAddress, a.IPAddress+" is released")
		},
	}
}

func addressAttachCmd(r *run) *cobra.Command {
	var machine, network string
	cmd := &cobra.Command{
		Use:               "attach <ip> --vm <vm> [--network <network>]",
		Aliases:           []string{"nat", "static-nat"},
		Short:             "Point a public address at a VM (static NAT)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: addressArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			a, err := c.FindAddress(ctx, args[0])
			if err != nil {
				return err
			}
			m, err := c.GetMachine(ctx, machine)
			if err != nil {
				return err
			}
			networkID, err := natNetwork(ctx, c, a, m, network)
			if err != nil {
				return err
			}
			accepted, err := c.EnableStaticNAT(ctx, a.ID, m.ID, networkID)
			if err != nil {
				return err
			}
			return r.waitNet(ctx, c, accepted, "Pointing "+a.IPAddress+" at "+m.Name, a.IPAddress+" now reaches "+m.Name)
		},
	}
	cmd.Flags().StringVar(&machine, "vm", "", "the VM (name or id)")
	cmd.Flags().StringVar(&network, "network", "", "the VM's network to forward to (name or id; found when only one is in the address's VPC)")
	_ = cmd.MarkFlagRequired("vm")
	netCompleteFlag(r, cmd, "vm", "vm", listMachineNames)
	netCompleteFlag(r, cmd, "network", "network", listNetworkNames)
	return cmd
}

// natNetwork is the network static NAT forwards to: the one asked for, or,
// for a machine on several networks, its only one in the address's VPC.
// Empty leaves the choice to the cloud.
func natNetwork(ctx context.Context, c *wec.Client, a *wec.Address, m *wec.Machine, network string) (string, error) {
	if network != "" {
		n, err := c.FindNetwork(ctx, network)
		if err != nil {
			return "", err
		}
		return n.ID, nil
	}
	if len(m.Addresses) < 2 || a.VPCID == "" {
		return "", nil
	}
	networks, err := c.ListNetworks(ctx, wec.ListOptions{})
	if err != nil {
		return "", nil
	}
	inVPC := map[string]bool{}
	for _, n := range networks {
		inVPC[n.ID] = n.VPCID == a.VPCID
	}
	var found []string
	for _, nic := range m.Addresses {
		if inVPC[nic.NetworkID] {
			found = append(found, nic.NetworkID)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return "", nil
}

func addressDetachCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "detach <ip>",
		Aliases:           []string{"unnat"},
		Short:             "Stop pointing a public address at its VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: addressArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			a, err := c.FindAddress(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.StaticNATMachineID == "" {
				return usagef("%s isn't pointed at a VM", a.IPAddress)
			}
			accepted, err := c.DisableStaticNAT(cmd.Context(), a.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Detaching "+a.IPAddress,
				a.IPAddress+" no longer reaches "+a.StaticNATMachineName)
		},
	}
}
