package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func vpnCmd(r *run) *cobra.Command {
	return group("vpn", "Site-to-site VPN: tunnels from a VPC to your offices", []string{"vpns"},
		vpnGetCmd(r), vpnEnableCmd(r), vpnDisableCmd(r), vpnConnectCmd(r), vpnResetCmd(r),
		vpnDisconnectCmd(r), vpnSettingsCmd(r), vpnLinkCmd(r), vpnUnlinkCmd(r), vpnOfficeCmd(r))
}

func vpnOfficeCmd(r *run) *cobra.Command {
	return group("office", "Your offices' routers (customer gateways), shared by every VPC", []string{"offices"},
		officeListCmd(r), officeGetCmd(r), officeCreateCmd(r), officeUpdateCmd(r), officeDeleteCmd(r))
}

func officeArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("vpn-office", func(ctx context.Context, c *wec.Client) ([]string, error) {
		offices, err := c.ListVPNOffices(ctx, wec.ListOptions{})
		names := make([]string, len(offices))
		for i, o := range offices {
			names[i] = o.Name
		}
		return names, err
	})
}

// tunnelState colours a connection's state. A passive tunnel that is down is
// waiting for their router to dial in, so it reads Waiting, as in the dashboard.
func tunnelState(s string, passive bool) string {
	switch strings.ToLower(s) {
	case "connected":
		return ui.Green.Render(s)
	case "error":
		return ui.Red.Render(s)
	case "disconnected":
		if passive {
			return ui.Dim.Render("Waiting")
		}
		return ui.Yellow.Render(s)
	}
	return ui.State(s)
}

func vpnGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <vpc>",
		Aliases:           []string{"show", "status"},
		Short:             "Show a VPC's VPN: its address, limits and tunnels",
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
			vpn, err := c.GetVPCVPN(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			return r.show(vpn, connectionIDs(vpn), func() string { return vpnTable(r, v, vpn) })
		},
	}
}

func connectionIDs(vpn *wec.VPCVPN) []string {
	ids := make([]string, len(vpn.Connections))
	for i, x := range vpn.Connections {
		ids[i] = x.ID
	}
	return ids
}

func vpnTable(r *run, v *wec.VPC, vpn *wec.VPCVPN) string {
	if vpn.Gateway == nil {
		return ui.Dim.Render(v.Name+" has no VPN.") + "\n" +
			ui.Dim.Render(r.app.Refs("  turn it on with: gowec vpn enable "+v.Name)) + "\n"
	}
	out := ui.Pairs([][2]string{
		{"VPC", ui.Bold.Render(v.Name)},
		{"Our address", vpn.Gateway.PublicIP},
		{"Our range", dash(vpn.Gateway.VPCCIDR)},
		{"Tunnels", fmt.Sprintf("%d of %d", len(vpn.Connections), vpn.Limits.Connections)},
	})
	if len(vpn.Connections) == 0 {
		return out + "\n" + ui.Dim.Render(r.app.Refs("  connect an office with: gowec vpn connect "+v.Name+" --office <office>")) + "\n"
	}
	rows := make([][]string, len(vpn.Connections))
	for i, x := range vpn.Connections {
		state := tunnelState(x.State, x.Passive)
		if x.Passive {
			state += "\n" + ui.Dim.Render("passive")
		}
		office := ui.Bold.Render(dash(x.Office.Name)) + "\n" + ui.Dim.Render(x.Office.Gateway)
		if x.PeerVPC != nil {
			office = ui.Bold.Render(dash(x.PeerVPC.Name)) + "\n" + ui.Dim.Render("your VPC · "+x.Office.Gateway)
		}
		rows[i] = []string{office,
			strings.Join(x.Office.CIDRList, "\n"), state, ui.Dim.Render(x.ID)}
	}
	return out + "\n" + ui.RowTable([]string{"OFFICE", "THEIR RANGES", "STATE", "ID"}, rows)
}

func vpnEnableCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "enable <vpc>",
		Short:             "Turn on a VPC's VPN (it uses the VPC's source NAT address)",
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
			accepted, err := c.EnableVPN(cmd.Context(), v.ID, r.idempotencyKey())
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Enabling the VPN on "+v.Name, v.Name+"'s VPN is on")
		},
	}
}

func vpnDisableCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "disable <vpc>",
		Aliases:           []string{"remove"},
		Short:             "Turn off a VPC's VPN (disconnect its offices first)",
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
			if err := r.confirm("Turn off the VPN on "+v.Name+"?", "turning off a VPN"); err != nil {
				return err
			}
			accepted, err := c.DisableVPN(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Turning off the VPN on "+v.Name, v.Name+"'s VPN is off")
		},
	}
}

// officeFlags are an office's settings as flags, shared by create, update
// and connect. The key is read from a file or asked for, never a flag value,
// so it stays out of shell history.
type officeFlags struct {
	body    wec.CreateVPNOffice
	pskFile string
}

func (o *officeFlags) add(f *pflag.FlagSet) {
	f.StringVar(&o.body.Name, "name", "", "the office's name (default: VPN-<peer>)")
	f.StringVar(&o.body.Gateway, "peer", "", "their router's public IPv4 address or hostname")
	f.StringSliceVar(&o.body.CIDRList, "ranges", nil, "their private ranges, comma-separated (e.g. 172.20.0.0/16)")
	f.StringVar(&o.pskFile, "psk-file", "", "a file holding the pre-shared key, or - for stdin (asked for when left out)")
	f.StringVar(&o.body.IKEPolicy, "ike-policy", "aes256-sha256;modp2048", "phase 1: cipher-hash;group")
	f.StringVar(&o.body.ESPPolicy, "esp-policy", "aes256-sha256;modp2048", "phase 2: cipher-hash[;group]")
	f.StringVar(&o.body.IKEVersion, "ike-version", "ikev2", "ike, ikev1 or ikev2")
	f.IntVar(&o.body.IKELifetime, "ike-lifetime", 86400, "phase 1 lifetime in seconds")
	f.IntVar(&o.body.ESPLifetime, "esp-lifetime", 3600, "phase 2 lifetime in seconds")
	f.BoolVar(&o.body.DPD, "dpd", false, "dead peer detection")
	f.BoolVar(&o.body.ForceEncap, "force-encap", false, "always wrap ESP in UDP 4500")
	f.BoolVar(&o.body.SplitConnections, "split-connections", false, "one tunnel per range pair")
}

// psk reads the pre-shared key from --psk-file, or asks for it on a terminal.
func (o *officeFlags) psk(r *run) (string, error) {
	if o.pskFile != "" {
		var data []byte
		var err error
		if o.pskFile == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(o.pskFile)
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	key, err := r.app.Prompter.Secret(ui.Accent.Render("?") + " Pre-shared key " + ui.Dim.Render("(hidden)") + ": ")
	if err == ui.ErrNotInteractive {
		return "", usagef("no terminal to ask on: pass the key with --psk-file")
	}
	return strings.TrimSpace(key), err
}

// createOffice saves the office the flags describe and waits for it.
func (o *officeFlags) createOffice(ctx context.Context, r *run, c *wec.Client) (string, error) {
	if o.body.Gateway == "" || len(o.body.CIDRList) == 0 {
		return "", usagef("an office needs --peer and --ranges")
	}
	key, err := o.psk(r)
	if err != nil {
		return "", err
	}
	body := o.body
	body.IPsecPSK = key
	accepted, err := c.CreateVPNOffice(ctx, body, r.idempotencyKey())
	if err != nil {
		return "", err
	}
	if err := r.waitStep(ctx, c, accepted, "Saving the office"); err != nil {
		return "", err
	}
	r.app.ForgetNames()
	return accepted.ResourceID, nil
}

// waitStep follows a job that a later step depends on, so it waits even
// with --detach.
func (r *run) waitStep(ctx context.Context, c *wec.Client, accepted *wec.JobAccepted, doing string) error {
	if accepted.JobID == "" {
		return nil
	}
	spin := ui.StartSpinner(r.app.Err, r.tty(), doing+"…")
	job, err := c.WaitJob(ctx, accepted.JobID, nil)
	spin.Stop()
	if err != nil {
		return err
	}
	if job.Status == "failed" {
		return fmt.Errorf("%s failed; the account's events have the details · job %s", doing, job.JobID)
	}
	return nil
}

func vpnConnectCmd(r *run) *cobra.Command {
	var office string
	var passive bool
	var of officeFlags
	cmd := &cobra.Command{
		Use:   "connect <vpc> (--office <office> | --peer <ip> --ranges <cidrs>)",
		Short: "Connect a VPC to an office, turning its VPN on and saving the office if needed",
		Long: "Connect a VPC to an office. With --office it uses a saved one; with --peer and\n" +
			"--ranges it saves a new office first (the key is asked for, or read with --psk-file).\n" +
			"The VPC's VPN is turned on when it's off.",
		Example: "  gowec vpn connect prod --office austin-dc\n" +
			"  gowec vpn connect prod --name austin-dc --peer 52.10.20.7 --ranges 172.20.0.0/16 --psk-file key.txt",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (office == "") == (of.body.Gateway == "") {
				return usagef("pass --office for a saved office, or --peer and --ranges for a new one")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, err := c.FindVPC(ctx, args[0])
			if err != nil {
				return err
			}
			officeID := ""
			if office != "" {
				o, err := c.FindVPNOffice(ctx, office)
				if err != nil {
					return err
				}
				officeID = o.ID
			}
			vpn, err := c.GetVPCVPN(ctx, v.ID)
			if err != nil {
				return err
			}
			if vpn.Gateway == nil {
				accepted, err := c.EnableVPN(ctx, v.ID, r.idempotencyKey())
				if err != nil {
					return err
				}
				if err := r.waitStep(ctx, c, accepted, "Enabling the VPN on "+v.Name); err != nil {
					return err
				}
			}
			created := false
			if officeID == "" {
				if officeID, err = of.createOffice(ctx, r, c); err != nil {
					return err
				}
				created = true
			}
			accepted, err := c.ConnectVPN(ctx, v.ID, officeID, passive, r.idempotencyKey())
			if err != nil {
				if created { // don't leave a new office behind that nothing uses
					_, _ = c.DeleteVPNOffice(ctx, officeID)
					r.app.ForgetNames()
				}
				return err
			}
			return r.waitNet(ctx, c, accepted, "Connecting "+v.Name, v.Name+" is connected · check it with gowec vpn get "+v.Name)
		},
	}
	f := cmd.Flags()
	f.StringVar(&office, "office", "", "a saved office, name or id (gowec vpn office list)")
	f.BoolVar(&passive, "passive", false, "wait for their side to start the tunnel")
	of.add(f)
	_ = cmd.RegisterFlagCompletionFunc("office", func(c *cobra.Command, args []string, s string) ([]string, cobra.ShellCompDirective) {
		return officeArg(r)(c, nil, s)
	})
	return cmd
}

// vpnConnection resolves a VPC and one of its connections (id or office name).
func vpnConnection(ctx context.Context, c *wec.Client, vpc, ref string) (*wec.VPC, *wec.VPNConnection, error) {
	v, err := c.FindVPC(ctx, vpc)
	if err != nil {
		return nil, nil, err
	}
	x, err := c.FindVPNConnection(ctx, v.ID, ref)
	return v, x, err
}

func vpnResetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "reset <vpc> <office|connection-id>",
		Short:             "Reset a tunnel: drop it and negotiate it again",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, x, err := vpnConnection(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			accepted, err := c.ResetVPNConnection(cmd.Context(), v.ID, x.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Resetting the tunnel to "+x.Office.Name, "The tunnel to "+x.Office.Name+" was reset")
		},
	}
}

func vpnDisconnectCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "disconnect <vpc> <office|connection-id>",
		Short:             "Delete a tunnel (the office stays saved)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, x, err := vpnConnection(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Disconnect %s from %s? Traffic between them stops.", v.Name, x.Office.Name),
				"disconnecting a tunnel"); err != nil {
				return err
			}
			accepted, err := c.DeleteVPNConnection(cmd.Context(), v.ID, x.ID)
			if err != nil {
				return err
			}
			return r.waitNet(cmd.Context(), c, accepted, "Disconnecting "+x.Office.Name, v.Name+" is disconnected from "+x.Office.Name)
		},
	}
}

// waitOperation follows a link or unlink to its end, as waitJob does a job.
func (r *run) waitOperation(ctx context.Context, c *wec.Client, op *wec.Operation, doing, done string) error {
	if r.detach && op.OperationID != "" {
		return r.show(op, []string{op.OperationID}, func() string {
			return ui.Success(doing+" · operation "+op.OperationID) + "\n"
		})
	}
	spin := ui.StartSpinner(r.app.Err, r.tty(), doing+"…")
	op, err := c.WaitOperation(ctx, op, nil)
	spin.Stop()
	if err != nil {
		return err
	}
	if op.Status == "failed" {
		return fmt.Errorf("%s", dash(op.ErrorDetail))
	}
	return r.show(op, []string{op.OperationID}, func() string { return ui.Success(done) + "\n" })
}

func vpnLinkCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:   "link <vpc> <other-vpc>",
		Short: "Link two of your VPCs over the VPN: both ends, one key nobody needs to copy",
		Long: "Link two of your VPCs over the VPN. Both need their VPN on, and their ranges must not overlap.\n" +
			"We save an office on each (the other VPC), share one key nobody sees, and connect them:\n" +
			"the second VPC waits and the first dials out.",
		Args:              cobra.ExactArgs(2),
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
			peer, err := c.FindVPC(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			op, err := c.LinkVPC(cmd.Context(), v.ID, peer.ID, r.idempotencyKey())
			if err != nil {
				return err
			}
			return r.waitOperation(cmd.Context(), c, op, "Linking "+v.Name+" and "+peer.Name,
				v.Name+" and "+peer.Name+" are linked; check the tunnel with: gowec vpn get "+v.Name)
		},
	}
}

// vpnLink finds a VPC's link by the other VPC's name or id, or the connection's id.
func vpnLink(ctx context.Context, c *wec.Client, vpc, ref string) (*wec.VPC, *wec.VPNConnection, error) {
	v, err := c.FindVPC(ctx, vpc)
	if err != nil {
		return nil, nil, err
	}
	vpn, err := c.GetVPCVPN(ctx, v.ID)
	if err != nil {
		return nil, nil, err
	}
	for i, x := range vpn.Connections {
		if x.PeerVPC != nil && (x.ID == ref || x.PeerVPC.ID == ref || strings.EqualFold(x.PeerVPC.Name, ref)) {
			return v, &vpn.Connections[i], nil
		}
	}
	return nil, nil, fmt.Errorf("%s has no link to %q; see its tunnels with: gowec vpn get %s", v.Name, ref, v.Name)
}

func vpnUnlinkCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "unlink <vpc> <other-vpc|connection-id>",
		Short:             "Remove a link between two of your VPCs: both tunnels and both offices",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, x, err := vpnLink(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			peer := x.PeerVPC.Name
			if err := r.confirm(fmt.Sprintf("Unlink %s and %s? Traffic between them stops, on both VPCs.", v.Name, peer),
				"removing a link"); err != nil {
				return err
			}
			op, err := c.UnlinkVPC(cmd.Context(), v.ID, x.ID)
			if err != nil {
				return err
			}
			return r.waitOperation(cmd.Context(), c, op, "Unlinking "+v.Name+" and "+peer, v.Name+" and "+peer+" are unlinked")
		},
	}
}

func vpnSettingsCmd(r *run) *cobra.Command {
	var showKey bool
	cmd := &cobra.Command{
		Use:   "settings <vpc> <office|connection-id>",
		Short: "Show what their router needs for a tunnel, the key included (needs network:manage)",
		Long: "Show what their router needs for a tunnel. The key prints masked unless --show-key;\n" +
			"-o json and -o yaml print it as the API returns it. Every read is recorded in the account's events.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: vpcArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			v, x, err := vpnConnection(cmd.Context(), c, args[0], args[1])
			if err != nil {
				return err
			}
			s, err := c.GetVPNRouterSettings(cmd.Context(), v.ID, x.ID)
			if err != nil {
				return err
			}
			return r.show(s, []string{x.ID}, func() string {
				key := ui.Dim.Render("•••••••••••••••• (--show-key)")
				if showKey {
					key = dash(s.IPsecPSK)
				}
				dpd := "Off"
				if s.Office.DPD {
					dpd = "On"
				}
				return ui.Pairs([][2]string{
					{"Peer address (ours)", s.PublicIP},
					{"Remote network (ours)", dash(s.VPCCIDR)},
					{"Local networks (yours)", strings.Join(s.Office.CIDRList, ", ")},
					{"Pre-shared key", key},
					{"IKE version", dash(s.Office.IKEVersion)},
					{"IKE proposal", dash(s.Office.IKEPolicy)},
					{"IKE lifetime", fmt.Sprintf("%d s", s.Office.IKELifetime)},
					{"ESP proposal", dash(s.Office.ESPPolicy)},
					{"ESP lifetime", fmt.Sprintf("%d s", s.Office.ESPLifetime)},
					{"Dead peer detection", dpd},
				})
			})
		},
	}
	cmd.Flags().BoolVar(&showKey, "show-key", false, "print the pre-shared key")
	return cmd
}

func officeListCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List offices and the VPCs each connects to",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			offices, err := c.ListVPNOffices(cmd.Context(), wec.ListOptions{})
			if err != nil {
				return err
			}
			ids := make([]string, len(offices))
			rows := make([][]string, len(offices))
			for i, o := range offices {
				ids[i] = o.ID
				var vpcs []string
				for _, x := range o.Connections {
					vpcs = append(vpcs, dash(x.VPCName)+"  "+tunnelState(x.State, x.Passive))
				}
				where := ui.Dim.Render("Not connected")
				if len(vpcs) > 0 {
					where = strings.Join(vpcs, "\n")
				}
				rows[i] = []string{ui.Bold.Render(dash(o.Name)) + "\n" + ui.Dim.Render(o.Gateway),
					strings.Join(o.CIDRList, "\n"), where, ui.Dim.Render(o.ID)}
			}
			return r.show(offices, ids, func() string {
				if len(offices) == 0 {
					return ui.Dim.Render("No offices.") + "\n"
				}
				return ui.RowTable([]string{"OFFICE", "THEIR RANGES", "CONNECTED TO", "ID"}, rows)
			})
		},
	}
}

func officeGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <office>",
		Aliases:           []string{"show", "describe"},
		Short:             "Show an office's settings (never its key)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: officeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			o, err := c.FindVPNOffice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			onOff := func(b bool) string {
				if b {
					return "on"
				}
				return ui.Dim.Render("off")
			}
			pairs := [][2]string{
				{"Name", ui.Bold.Render(dash(o.Name))},
				{"ID", ui.Dim.Render(o.ID)},
				{"Their router", o.Gateway},
				{"Their ranges", strings.Join(o.CIDRList, ", ")},
				{"IKE", fmt.Sprintf("%s  %s  %ds", o.IKEVersion, o.IKEPolicy, o.IKELifetime)},
				{"ESP", fmt.Sprintf("%s  %ds", o.ESPPolicy, o.ESPLifetime)},
				{"Dead peer detection", onOff(o.DPD)},
				{"Force UDP encapsulation", onOff(o.ForceEncap)},
				{"Split connections", onOff(o.SplitConnections)},
			}
			for _, x := range o.Connections {
				pairs = append(pairs, [2]string{"Connected to", dash(x.VPCName) + "  " + tunnelState(x.State, x.Passive)})
			}
			return r.show(o, []string{o.ID}, func() string { return ui.Pairs(pairs) })
		},
	}
}

func officeCreateCmd(r *run) *cobra.Command {
	var of officeFlags
	cmd := &cobra.Command{
		Use:     "create --peer <ip> --ranges <cidrs> [--name <name>]",
		Aliases: []string{"new"},
		Short:   "Save an office without connecting it (the key is asked for, or read with --psk-file)",
		Example: "  gowec vpn office create --name austin-dc --peer 52.10.20.7 --ranges 172.20.0.0/16,172.21.0.0/16",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if of.body.Gateway == "" || len(of.body.CIDRList) == 0 {
				return usagef("an office needs --peer and --ranges")
			}
			key, err := of.psk(r)
			if err != nil {
				return err
			}
			body := of.body
			body.IPsecPSK = key
			accepted, err := c.CreateVPNOffice(cmd.Context(), body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			name := body.Name
			if name == "" {
				name = "VPN-" + body.Gateway
			}
			return r.waitNet(cmd.Context(), c, accepted, "Saving "+name, "Saved "+name)
		},
	}
	of.add(cmd.Flags())
	return cmd
}

func officeUpdateCmd(r *run) *cobra.Command {
	var of officeFlags
	cmd := &cobra.Command{
		Use:               "update <office> [flags]",
		Aliases:           []string{"edit"},
		Short:             "Change an office; only the flags you pass change (restarts its tunnels on every VPC)",
		Example:           "  gowec vpn office update austin-dc --ranges 172.20.0.0/16,172.22.0.0/16 --dpd",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: officeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			o, err := c.FindVPNOffice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			body, err := of.changes(r, cmd.Flags())
			if err != nil {
				return err
			}
			if len(o.Connections) > 0 {
				if err := r.confirm(fmt.Sprintf("Save %s? Its tunnels restart on %s.", o.Name,
					countNoun(len(o.Connections), "VPC")), "restarting tunnels"); err != nil {
					return err
				}
			}
			accepted, err := c.UpdateVPNOffice(cmd.Context(), o.ID, body)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Saving "+o.Name, "Saved "+o.Name)
		},
	}
	of.add(cmd.Flags())
	return cmd
}

// changes is the PATCH body: only the flags the person passed.
func (o *officeFlags) changes(r *run, f *pflag.FlagSet) (wec.UpdateVPNOffice, error) {
	var u wec.UpdateVPNOffice
	b := o.body
	changed := false
	set := func(name string) bool {
		if f.Changed(name) {
			changed = true
			return true
		}
		return false
	}
	if set("name") {
		u.Name = &b.Name
	}
	if set("peer") {
		u.Gateway = &b.Gateway
	}
	if set("ranges") {
		u.CIDRList = b.CIDRList
	}
	if set("ike-policy") {
		u.IKEPolicy = &b.IKEPolicy
	}
	if set("esp-policy") {
		u.ESPPolicy = &b.ESPPolicy
	}
	if set("ike-version") {
		u.IKEVersion = &b.IKEVersion
	}
	if set("ike-lifetime") {
		u.IKELifetime = &b.IKELifetime
	}
	if set("esp-lifetime") {
		u.ESPLifetime = &b.ESPLifetime
	}
	if set("dpd") {
		u.DPD = &b.DPD
	}
	if set("force-encap") {
		u.ForceEncap = &b.ForceEncap
	}
	if set("split-connections") {
		u.SplitConnections = &b.SplitConnections
	}
	if set("psk-file") {
		key, err := o.psk(r)
		if err != nil {
			return u, err
		}
		u.IPsecPSK = &key
	}
	if !changed {
		return u, usagef("nothing to change: pass the settings to change, e.g. --ranges or --dpd")
	}
	return u, nil
}

func officeDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <office>",
		Aliases:           []string{"rm"},
		Short:             "Delete an office nothing connects to",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: officeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			o, err := c.FindVPNOffice(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm("Delete the office "+o.Name+"?", "deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteVPNOffice(cmd.Context(), o.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitNet(cmd.Context(), c, accepted, "Deleting "+o.Name, o.Name+" is deleted")
		},
	}
}
