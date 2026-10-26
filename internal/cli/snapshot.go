package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/WiLine-Edge-Cloud/gowec/internal/ui"
	"github.com/WiLine-Edge-Cloud/gowec/pkg/wec"
)

func snapshotCmd(r *run) *cobra.Command {
	machine := group("vm", "Snapshots of whole VMs", []string{"vms", "machine", "machines"},
		machineSnapshotListCmd(r), machineSnapshotCreateCmd(r),
		snapshotDeleteCmd(r, true), snapshotRevertCmd(r, true))
	policy := group("policy", "Volume snapshots on a schedule", []string{"policies", "schedule"},
		snapshotPolicyListCmd(r), snapshotPolicyCreateCmd(r), snapshotPolicyUpdateCmd(r), snapshotPolicyDeleteCmd(r))
	return group("snapshot", "Disk and VM snapshots", []string{"snapshots", "snap"},
		snapshotListCmd(r), snapshotCreateCmd(r), snapshotDeleteCmd(r, false), snapshotRevertCmd(r, false),
		machine, policy)
}

func snapshotArg(r *run, ofMachine bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	if ofMachine {
		return r.completeNames("VM-snapshot", func(ctx context.Context, c *wec.Client) ([]string, error) {
			snaps, err := c.ListMachineSnapshots(ctx, "", wec.ListOptions{})
			names := make([]string, len(snaps))
			for i, s := range snaps {
				names[i] = s.Label()
			}
			return names, err
		})
	}
	return r.completeNames("snapshot", func(ctx context.Context, c *wec.Client) ([]string, error) {
		snaps, err := c.ListSnapshots(ctx, "", wec.ListOptions{})
		names := make([]string, len(snaps))
		for i, s := range snaps {
			names[i] = s.Name
		}
		return names, err
	})
}

// storageSize is a byte count in GB, the way the portal shows disk sizes.
func storageSize(bytes int64) string {
	if bytes <= 0 {
		return ui.Dim.Render("—")
	}
	return gb(float64(bytes) / (1 << 30))
}

// storageSchedule reads a CloudStack schedule (mm, mm:HH, mm:HH:D or
// mm:HH:DD by interval) as a person would say it.
func storageSchedule(interval, schedule string) string {
	p := strings.Split(schedule, ":")
	at := func() string { return p[1] + ":" + p[0] }
	switch strings.ToUpper(interval) {
	case "HOURLY":
		if len(p) == 1 {
			return "hourly at :" + p[0]
		}
	case "DAILY":
		if len(p) == 2 {
			return "daily at " + at()
		}
	case "WEEKLY":
		days := map[string]string{"1": "Sun", "2": "Mon", "3": "Tue", "4": "Wed", "5": "Thu", "6": "Fri", "7": "Sat"}
		if len(p) == 3 && days[p[2]] != "" {
			return "weekly, " + days[p[2]] + " at " + at()
		}
	case "MONTHLY":
		if len(p) == 3 {
			return "monthly, day " + strings.TrimLeft(p[2], "0") + " at " + at()
		}
	}
	return strings.ToLower(interval) + " " + schedule
}

// storageInterval checks an --interval value and puts it in the API's case.
func storageInterval(v string) (string, error) {
	switch up := strings.ToUpper(v); up {
	case "HOURLY", "DAILY", "WEEKLY", "MONTHLY":
		return up, nil
	}
	return "", usagef("--interval must be hourly, daily, weekly or monthly")
}

var weekdays = map[string]string{"sun": "1", "mon": "2", "tue": "3", "wed": "4", "thu": "5", "fri": "6", "sat": "7"}

// storageWhen holds a schedule's --at and --day.
type storageWhen struct{ at, day string }

func (w *storageWhen) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&w.at, "at", "", "the time, HH:MM (for hourly, the minute)")
	cmd.Flags().StringVar(&w.day, "day", "", "weekly: the weekday (mon…sun); monthly: the day of the month (1-28)")
	_ = cmd.MarkFlagRequired("at")
	_ = cmd.RegisterFlagCompletionFunc("day", fixed("mon", "tue", "wed", "thu", "fri", "sat", "sun"))
}

// schedule turns --at and --day into CloudStack's form, which puts minutes
// first: MM (hourly), MM:HH (daily), MM:HH:D (weekly, 1 = Sunday), MM:HH:DD (monthly).
func (w *storageWhen) schedule(interval string) (string, error) {
	hour, minute, hasHour := strings.Cut(strings.TrimPrefix(w.at, ":"), ":")
	if !hasHour {
		hour, minute = "", hour
	}
	h, herr := strconv.Atoi(hour)
	m, merr := strconv.Atoi(minute)
	if merr != nil || m < 0 || m > 59 || (hasHour && (herr != nil || h < 0 || h > 23)) {
		return "", usagef("--at must be a time, HH:MM (or the minute, for hourly)")
	}
	if interval == "HOURLY" {
		return fmt.Sprintf("%02d", m), nil
	}
	if !hasHour {
		return "", usagef("--at must be HH:MM for a %s schedule", strings.ToLower(interval))
	}
	at := fmt.Sprintf("%02d:%02d", m, h)
	switch interval {
	case "WEEKLY":
		d := weekdays[strings.ToLower(w.day)[:min(3, len(w.day))]]
		if d == "" {
			return "", usagef("a weekly schedule needs --day mon…sun")
		}
		return at + ":" + d, nil
	case "MONTHLY":
		d, err := strconv.Atoi(w.day)
		if err != nil || d < 1 || d > 28 {
			return "", usagef("a monthly schedule needs --day 1-28")
		}
		return fmt.Sprintf("%s:%02d", at, d), nil
	}
	return at, nil
}

func snapshotListCmd(r *run) *cobra.Command {
	var volume, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List volume snapshots",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			var volumeID string
			if volume != "" {
				if volumeID, err = c.ResolveVolumeID(cmd.Context(), volume); err != nil {
					return err
				}
			}
			snaps, err := c.ListSnapshots(cmd.Context(), volumeID, wec.ListOptions{Status: status})
			if err != nil {
				return err
			}
			ids := make([]string, len(snaps))
			for i, s := range snaps {
				ids[i] = s.ID
			}
			return r.show(snaps, ids, func() string {
				if len(snaps) == 0 {
					return ui.Dim.Render("No snapshots.") + "\n"
				}
				now := time.Now()
				rows := make([][]string, len(snaps))
				for i, s := range snaps {
					kind := strings.ToLower(s.IntervalType)
					if kind == "" {
						kind = strings.ToLower(s.Type)
					}
					rows[i] = []string{ui.Bold.Render(s.Name) + "\n" + ui.Dim.Render(kind), dash(s.VolumeName),
						ui.State(s.State), storageSize(s.PhysicalSize), created(s.Created, now), ui.Dim.Render(s.ID)}
				}
				return ui.RowTable([]string{"NAME", "VOLUME", "STATE", "SIZE", "CREATED", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&volume, "volume", "", "only this volume's snapshots (name or id)")
	cmd.Flags().StringVar(&status, "status", "", "only snapshots in this state (backedup, …)")
	return cmd
}

func snapshotCreateCmd(r *run) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "create <volume> [--name <name>]",
		Aliases:           []string{"new", "take"},
		Short:             "Snapshot a volume",
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
			accepted, err := c.CreateSnapshot(cmd.Context(), id, name, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitJob(cmd.Context(), c, accepted, "Snapshotting "+args[0], args[0]+" is snapshotted")
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the snapshot's name (WEC picks one otherwise)")
	return cmd
}

func machineSnapshotListCmd(r *run) *cobra.Command {
	var machine, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List VM snapshots",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			var machineID string
			if machine != "" {
				m, err := c.GetMachine(cmd.Context(), machine)
				if err != nil {
					return err
				}
				machineID = m.ID
			}
			snaps, err := c.ListMachineSnapshots(cmd.Context(), machineID, wec.ListOptions{Status: status})
			if err != nil {
				return err
			}
			ids := make([]string, len(snaps))
			for i, s := range snaps {
				ids[i] = s.ID
			}
			return r.show(snaps, ids, func() string {
				if len(snaps) == 0 {
					return ui.Dim.Render("No VM snapshots.") + "\n"
				}
				now := time.Now()
				rows := make([][]string, len(snaps))
				for i, s := range snaps {
					label := ui.Bold.Render(s.Label())
					if s.Current {
						label += " " + ui.Accent.Render("current")
					}
					kind := "disks"
					if strings.Contains(strings.ToLower(s.Type), "memory") {
						kind = "disks and memory"
					}
					rows[i] = []string{label + "\n" + ui.Dim.Render(clip(s.Description, 40)),
						dash(s.MachineName), ui.State(s.State), kind, created(s.Created, now), ui.Dim.Render(s.ID)}
				}
				return ui.RowTable([]string{"NAME", "VM", "STATE", "HOLDS", "CREATED", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&machine, "vm", "", "only this VM's snapshots (name or id)")
	cmd.Flags().StringVar(&status, "status", "", "only snapshots in this state (ready, …)")
	storageMachineFlag(r, cmd)
	return cmd
}

func machineSnapshotCreateCmd(r *run) *cobra.Command {
	var body wec.CreateMachineSnapshot
	var memory bool
	cmd := &cobra.Command{
		Use:               "create <vm> [--name <name>]",
		Aliases:           []string{"new", "take"},
		Short:             "Snapshot a whole VM",
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
			body.MachineID = m.ID
			if cmd.Flags().Changed("memory") {
				body.IncludeMemory = &memory
			}
			accepted, err := c.CreateMachineSnapshot(cmd.Context(), body, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitJob(cmd.Context(), c, accepted, "Snapshotting "+m.Name, m.Name+" is snapshotted")
		},
	}
	cmd.Flags().StringVar(&body.Name, "name", "", "the snapshot's name")
	cmd.Flags().StringVar(&body.Description, "description", "", "a note on what it holds")
	cmd.Flags().BoolVar(&memory, "memory", false, "also keep the running VM's memory")
	return cmd
}

// snapshotDeleteCmd deletes a volume snapshot, or a machine snapshot.
func snapshotDeleteCmd(r *run, ofMachine bool) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <snapshot>",
		Aliases:           []string{"rm"},
		Short:             "Delete a snapshot (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: snapshotArg(r, ofMachine),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			resolveID, del := c.ResolveSnapshotID, c.DeleteSnapshot
			if ofMachine {
				resolveID, del = c.ResolveMachineSnapshotID, c.DeleteMachineSnapshot
			}
			id, err := resolveID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm("Delete the snapshot "+args[0]+"?", "deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := del(cmd.Context(), id)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.storageJob(cmd.Context(), c, accepted, "Deleting "+args[0], args[0]+" is deleted")
		},
	}
}

// snapshotRevertCmd puts a volume, or a whole machine, back to a snapshot.
func snapshotRevertCmd(r *run, ofMachine bool) *cobra.Command {
	what, short := "its volume", "Put a volume back to a snapshot (what was written since is lost)"
	if ofMachine {
		what, short = "its VM", "Put a VM back to a snapshot (what changed since is lost)"
	}
	return &cobra.Command{
		Use:               "revert <snapshot>",
		Aliases:           []string{"restore"},
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: snapshotArg(r, ofMachine),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			resolveID, revert := c.ResolveSnapshotID, c.RevertSnapshot
			if ofMachine {
				resolveID, revert = c.ResolveMachineSnapshotID, c.RevertMachineSnapshot
			}
			id, err := resolveID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Revert %s to %s? What changed since is lost.", what, args[0]),
				"reverting overwrites data"); err != nil {
				return err
			}
			accepted, err := revert(cmd.Context(), id)
			if err != nil {
				return err
			}
			return r.storageJob(cmd.Context(), c, accepted, "Reverting to "+args[0], "Reverted to "+args[0])
		},
	}
}

func snapshotPolicyListCmd(r *run) *cobra.Command {
	var volume string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List snapshot schedules",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			var volumeID string
			if volume != "" {
				if volumeID, err = c.ResolveVolumeID(cmd.Context(), volume); err != nil {
					return err
				}
			}
			policies, err := c.ListSnapshotPolicies(cmd.Context(), volumeID)
			if err != nil {
				return err
			}
			names := map[string]string{}
			if r.output == "table" && !r.quiet && len(policies) > 0 {
				if vols, err := c.ListVolumes(cmd.Context(), wec.ListOptions{}); err == nil {
					for _, v := range vols {
						names[v.ID] = v.Name
					}
				}
			}
			ids := make([]string, len(policies))
			rows := make([][]string, len(policies))
			for i, p := range policies {
				ids[i] = p.ID
				vol := names[p.VolumeID]
				if vol == "" {
					vol = ui.Dim.Render(p.VolumeID)
				}
				rows[i] = []string{vol, storageSchedule(p.IntervalType, p.Schedule), dash(p.Timezone),
					fmt.Sprint(p.MaxSnapshots), ui.Dim.Render(p.ID)}
			}
			return r.show(policies, ids, func() string {
				if len(policies) == 0 {
					return ui.Dim.Render("No snapshot schedules.") + "\n"
				}
				return ui.Table([]string{"VOLUME", "WHEN", "TIMEZONE", "KEEPS", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&volume, "volume", "", "only this volume's schedules (name or id)")
	return cmd
}

func snapshotPolicyCreateCmd(r *run) *cobra.Command {
	var body wec.CreateSnapshotPolicy
	var when storageWhen
	cmd := &cobra.Command{
		Use:     "create <volume> --interval <interval> --at <HH:MM> [--day <day>] --keep <n>",
		Aliases: []string{"new"},
		Short:   "Snapshot a volume on a schedule",
		Example: "  gowec snapshot policy create data-1 --interval daily --at 03:00 --keep 7\n" +
			"  gowec snapshot policy create data-1 --interval weekly --at 23:30 --day sun --keep 4",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: volumeArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			if body.IntervalType, err = storageInterval(body.IntervalType); err != nil {
				return err
			}
			if body.Schedule, err = when.schedule(body.IntervalType); err != nil {
				return err
			}
			c, err := r.client()
			if err != nil {
				return err
			}
			if body.VolumeID, err = c.ResolveVolumeID(cmd.Context(), args[0]); err != nil {
				return err
			}
			p, err := c.CreateSnapshotPolicy(cmd.Context(), body)
			if err != nil {
				return err
			}
			return r.show(p, []string{p.ID}, func() string {
				return ui.Success(fmt.Sprintf("%s is snapshotted %s %s, the last %d kept", args[0],
					storageSchedule(p.IntervalType, p.Schedule), p.Timezone, p.MaxSnapshots)) + "\n"
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&body.IntervalType, "interval", "", "hourly, daily, weekly or monthly")
	when.flags(cmd)
	f.StringVar(&body.Timezone, "timezone", "UTC", "the schedule's timezone, e.g. America/Los_Angeles")
	f.IntVar(&body.MaxSnapshots, "keep", 0, "how many snapshots to keep before the oldest goes")
	for _, flag := range []string{"interval", "keep"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	_ = cmd.RegisterFlagCompletionFunc("interval", fixed("hourly", "daily", "weekly", "monthly"))
	return cmd
}

func snapshotPolicyUpdateCmd(r *run) *cobra.Command {
	var display bool
	cmd := &cobra.Command{
		Use:   "update <policy-id> --display=true|false",
		Short: "Show or hide a snapshot schedule in the dashboard",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			accepted, err := c.UpdateSnapshotPolicy(cmd.Context(), args[0], display)
			if err != nil {
				return err
			}
			done := "The schedule is shown in the dashboard"
			if !display {
				done = "The schedule is hidden from the dashboard"
			}
			return r.storageJob(cmd.Context(), c, accepted, "Updating the schedule", done)
		},
	}
	cmd.Flags().BoolVar(&display, "display", true, "show the schedule in the dashboard")
	_ = cmd.MarkFlagRequired("display")
	return cmd
}

func snapshotPolicyDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <policy-id>",
		Aliases: []string{"rm"},
		Short:   "Stop a snapshot schedule (the snapshots it took stay)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm("Stop this snapshot schedule? The snapshots it took stay.",
				"deleting a schedule"); err != nil {
				return err
			}
			if err := c.DeleteSnapshotPolicy(cmd.Context(), args[0]); err != nil {
				return err
			}
			return r.show(map[string]string{"id": args[0], "status": "deleted"}, []string{args[0]},
				func() string { return ui.Success("The schedule is deleted; its snapshots are kept") + "\n" })
		},
	}
}
