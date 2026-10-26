package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func volumeCmd(r *run) *cobra.Command {
	return group("volume", "Disks", []string{"volumes", "vol"},
		volumeListCmd(r), volumeGetCmd(r), volumeCreateCmd(r), volumeAttachCmd(r), volumeDetachCmd(r),
		volumeResizeCmd(r), volumeDeleteCmd(r))
}

// volumeArg completes a volume name as the first argument.
func volumeArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("volume", func(ctx context.Context, c *wec.Client) ([]string, error) {
		vols, err := c.ListVolumes(ctx, wec.ListOptions{})
		names := make([]string, len(vols))
		for i, v := range vols {
			names[i] = v.Name
		}
		return names, err
	})
}

// storageMachineFlag completes --machine with machine names.
func storageMachineFlag(r *run, cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("vm", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return r.app.cachedNames(r, "vm", listMachineNames), cobra.ShellCompDirectiveNoFileComp
	})
}

// storageJob follows a storage action's job. Some (deleting a volume,
// changing a policy) are done when they answer, with no job to follow.
func (r *run) storageJob(ctx context.Context, c *wec.Client, accepted *wec.JobAccepted, doing, done string) error {
	if accepted.JobID == "" {
		return r.show(accepted, []string{accepted.ResourceID}, func() string { return ui.Success(done) + "\n" })
	}
	return r.waitJob(ctx, c, accepted, doing, done)
}

func volumeKind(t string) string {
	switch strings.ToLower(t) {
	case "root":
		return "root disk"
	case "datadisk":
		return "data volume"
	}
	return strings.ToLower(t)
}

func volumeListCmd(r *run) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List volumes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			vols, err := c.ListVolumes(cmd.Context(), wec.ListOptions{Status: status})
			if err != nil {
				return err
			}
			ids := make([]string, len(vols))
			for i, v := range vols {
				ids[i] = v.ID
			}
			return r.show(vols, ids, func() string {
				if len(vols) == 0 {
					return ui.Dim.Render("No volumes.") + "\n"
				}
				now := time.Now()
				rows := make([][]string, len(vols))
				for i, v := range vols {
					machine := ui.Dim.Render("Detached")
					if v.MachineName != "" {
						machine = v.MachineName
					}
					rows[i] = []string{
						ui.Bold.Render(v.Name) + "\n" + ui.Dim.Render(volumeKind(v.Type)),
						fmt.Sprintf("%g GB", v.SizeGB) + "\n" + ui.Dim.Render(v.DiskOfferingName),
						ui.State(v.State), machine, created(v.Created, now), ui.Dim.Render(v.ID),
					}
				}
				return ui.RowTable([]string{"NAME", "SIZE", "STATE", "VM", "CREATED", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "only volumes in this state (ready, allocated, …)")
	return cmd
}

func volumeGetCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "get <volume>",
		Aliases:           []string{"show", "describe"},
		Short:             "Show one volume (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := c.ResolveVolumeID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			v, err := c.GetVolume(cmd.Context(), id)
			if err != nil {
				return err
			}
			return r.show(v, []string{v.ID}, func() string {
				machine := ui.Dim.Render("Detached")
				if v.MachineName != "" {
					machine = v.MachineName
				}
				return ui.Pairs([][2]string{
					{"Name", ui.Bold.Render(v.Name)},
					{"State", ui.State(v.State)},
					{"ID", ui.Dim.Render(v.ID)},
					{"Kind", volumeKind(v.Type)},
					{"Size", fmt.Sprintf("%g GB", v.SizeGB) + ui.Dim.Render("  "+v.DiskOfferingName)},
					{"VM", machine},
					{"Zone", zoneName(v.ZoneName)},
					{"Created", oneLine(created(v.Created, time.Now()))},
				})
			})
		},
	}
}

func volumeCreateCmd(r *run) *cobra.Command {
	var name, zone, offering, machine string
	var size, term int
	var price, maxPrice float64
	var noAutoRenew bool
	cmd := &cobra.Command{
		Use:     "create --name <name> --zone <zone> --offering <disk> [--term <months>]",
		Aliases: []string{"new"},
		Short:   "Make a data volume (on prepaid, a plan of its own that charges the wallet)",
		Long: "Make a data volume. On a prepaid account a volume is a plan of its own: pass --term.\n" +
			"gowec quotes the price and asks before charging the wallet; in scripts, pass --yes, or\n" +
			"--max-price to refuse anything dearer.",
		Example: "  gowec volume create --name data-1 --zone princeton --offering s-nvme-1-50gb --term 12\n" +
			"  gowec volume create --name scratch --zone princeton --offering Custom --size 200 --vm web-1",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			plan := cmd.Flags().Changed("term")
			switch {
			case plan && term != 1 && term != 12 && term != 24:
				return usagef("--term must be 1, 12 or 24 months")
			case !plan && maxPrice > 0:
				return usagef("--max-price goes with --term")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			zoneID, err := c.ResolveZoneID(ctx, zone)
			if err != nil {
				return err
			}
			disk, err := c.FindDiskOffering(ctx, offering, zoneID)
			if err != nil {
				return err
			}
			switch {
			case disk.CustomSize && size <= 0:
				return usagef("%s is a custom size: pass --size in GB", disk.Name)
			case !disk.CustomSize && size > 0:
				return usagef("--size is only for a custom-size offering; %s is %g GB", disk.Name, disk.SizeGB)
			}
			gbs := disk.SizeGB
			if disk.CustomSize {
				gbs = float64(size)
			}
			var m *wec.Machine
			if machine != "" {
				if m, err = c.GetMachine(ctx, machine); err != nil {
					return err
				}
			}

			if plan {
				quote, err := c.QuoteVolumePlan(ctx, disk.ID, size)
				if err != nil {
					return err
				}
				if price, err = quote.Total(term); err != nil {
					return err
				}
			}
			if r.output == "table" && !r.quiet {
				fmt.Fprint(r.app.Err, volumeSummary(name, zone, gbs, m, plan, term, price))
			}
			if !plan {
				if err := r.confirm(fmt.Sprintf("Create %s? It is billed while it exists.", name),
					"creating a volume can spend"); err != nil {
					return err
				}
				body := wec.CreateVolume{Name: name, ZoneID: zoneID, DiskOfferingID: disk.ID}
				if disk.CustomSize {
					body.SizeGB = size
				}
				if m != nil {
					body.MachineID = m.ID
				}
				accepted, err := c.CreateVolume(ctx, body, r.idempotencyKey())
				if wec.IsCode(err, "use_volume_plan") {
					fmt.Fprintln(r.app.Err, ui.Warn("On prepaid a volume is a plan of its own: add --term 1, 12 or 24."))
				}
				if err != nil {
					return err
				}
				r.app.ForgetNames()
				return r.waitJob(ctx, c, accepted, "Creating "+name, name+" is ready")
			}

			if maxPrice > 0 && price > maxPrice {
				return fmt.Errorf("the price, %s, is above --max-price %s; nothing was charged",
					money(price, ""), money(maxPrice, ""))
			}
			if maxPrice <= 0 {
				if err := r.confirm(fmt.Sprintf("Buy %s for %s from the wallet?", name, money(price, "")),
					"buying a volume charges the wallet"); err != nil {
					return err
				}
			}
			body := wec.BuyVolume{Name: name, ZoneID: zoneID, DiskOfferingID: disk.ID, TermMonths: term,
				AutoRenew: !noAutoRenew, QuotedTotal: price}
			if disk.CustomSize {
				body.SizeGB = size
			}
			if m != nil {
				body.VirtualMachineID = m.ID
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Buying "+name+"…")
			out, err := c.BuyVolumePlan(ctx, body, r.idempotencyKey())
			spin.Stop()
			if wec.IsCode(err, "plan_price_changed") {
				if now, ok := err.(*wec.APIError).Extra["quoted_total"].(float64); ok {
					fmt.Fprintln(r.app.Err, ui.Warn(fmt.Sprintf("The price moved to %s since the quote; nothing was charged. "+
						"Run it again to see it.", money(now, ""))))
				}
			}
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			id, _ := out["volume_id"].(string)
			return r.show(out, []string{id}, func() string { return volumeBought(r, name, out, m) })
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", "", "the volume's name")
	f.StringVar(&zone, "zone", "", "zone name or id (gowec zone list)")
	f.StringVar(&offering, "offering", "", "disk offering, name or id (gowec disk list)")
	f.IntVar(&size, "size", 0, "size in GB, for a custom-size offering")
	f.StringVar(&machine, "vm", "", "attach it to this VM (name or id), in the same zone")
	f.IntVar(&term, "term", 0, "prepaid: buy it as a plan for 1, 12 or 24 months")
	f.Float64Var(&maxPrice, "max-price", 0, "prepaid: buy without asking if the price is at most this; refuse otherwise")
	f.BoolVar(&noAutoRenew, "no-auto-renew", false, "prepaid: end the plan at its term instead of renewing it")
	for _, flag := range []string{"name", "zone", "offering"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	_ = cmd.RegisterFlagCompletionFunc("term", fixed("1", "12", "24"))
	storageMachineFlag(r, cmd)
	return cmd
}

func volumeSummary(name, zone string, gbs float64, m *wec.Machine, plan bool, term int, price float64) string {
	pairs := [][2]string{
		{"Volume", ui.Bold.Render(name)},
		{"Size", fmt.Sprintf("%g GB", gbs)},
		{"Zone", zoneName(zone)},
	}
	if m != nil {
		pairs = append(pairs, [2]string{"VM", m.Name})
	}
	if plan {
		months := "1 month"
		if term > 1 {
			months = fmt.Sprintf("%d months", term)
		}
		total := ui.Bold.Render(money(price, ""))
		if term > 1 {
			total += ui.Dim.Render(fmt.Sprintf("  (%s a month)", money(price/float64(term), "")))
		}
		pairs = append(pairs, [2]string{"Term", months}, [2]string{"Total", total})
	}
	return "\n" + ui.Pairs(pairs) + "\n"
}

// volumeBought says how a volume plan purchase came out.
func volumeBought(r *run, name string, out map[string]any, m *wec.Machine) string {
	msg := "Bought " + name
	if charged, ok := out["charged"].(float64); ok {
		msg += " · " + money(charged, "") + " charged"
	}
	lines := ui.Success(msg) + "\n"
	attached, _ := out["attached"].(bool)
	attaching, _ := out["attaching"].(bool)
	switch {
	case out["finishing"] == true:
		lines += ui.Warn(r.app.Refs("It is still being made; check it with gowec volume get "+name)) + "\n"
	case m != nil && attaching:
		lines += ui.Warn("It is still attaching to "+m.Name) + "\n"
	case m != nil && !attached:
		lines += ui.Warn(r.app.Refs("It wasn't attached to "+m.Name+"; try gowec volume attach "+name+" --vm "+m.Name)) + "\n"
	case m != nil:
		lines += ui.Dim.Render("  attached to "+m.Name) + "\n"
	}
	return lines
}

func volumeAttachCmd(r *run) *cobra.Command {
	var machine string
	cmd := &cobra.Command{
		Use:               "attach <volume> --vm <vm>",
		Short:             "Attach a data volume to a VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := c.ResolveVolumeID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			m, err := c.GetMachine(cmd.Context(), machine)
			if err != nil {
				return err
			}
			accepted, err := c.AttachVolume(cmd.Context(), id, m.ID)
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Attaching "+args[0]+" to "+m.Name,
				args[0]+" is attached to "+m.Name)
		},
	}
	cmd.Flags().StringVar(&machine, "vm", "", "the VM, by name or id")
	_ = cmd.MarkFlagRequired("vm")
	storageMachineFlag(r, cmd)
	return cmd
}

func volumeDetachCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "detach <volume>",
		Short:             "Detach a data volume from its VM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := c.ResolveVolumeID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			accepted, err := c.DetachVolume(cmd.Context(), id)
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Detaching "+args[0], args[0]+" is detached")
		},
	}
}

func volumeResizeCmd(r *run) *cobra.Command {
	var size int
	var offering string
	cmd := &cobra.Command{
		Use:               "resize <volume> --size <gb> | --offering <disk>",
		Short:             "Grow a volume (postpaid; it may cost more)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			if size <= 0 && offering == "" {
				return usagef("pass --size or --offering")
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := c.ResolveVolumeID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var offeringID, to string
			if offering != "" {
				disk, err := c.FindDiskOffering(cmd.Context(), offering, "")
				if err != nil {
					return err
				}
				offeringID, to = disk.ID, disk.Name
			}
			if size > 0 {
				to = fmt.Sprintf("%d GB", size)
			}
			if err := r.confirm(fmt.Sprintf("Grow %s to %s? It may cost more.", args[0], to),
				"resizing can spend"); err != nil {
				return err
			}
			accepted, err := c.ResizeVolume(cmd.Context(), id, size, offeringID, r.idempotencyKey())
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Resizing "+args[0], args[0]+" is now "+to)
		},
	}
	cmd.Flags().IntVar(&size, "size", 0, "the new size in GB (a custom-size offering)")
	cmd.Flags().StringVar(&offering, "offering", "", "a bigger disk offering, name or id (gowec disk list)")
	return cmd
}

func volumeDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <volume>",
		Aliases:           []string{"rm"},
		Short:             "Delete a detached volume (its data can't be recovered)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			id, err := c.ResolveVolumeID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Delete %s? Its data can't be recovered.", args[0]),
				"deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteVolume(cmd.Context(), id)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.storageJob(cmd.Context(), c, accepted, "Deleting "+args[0], args[0]+" is deleted")
		},
	}
}
