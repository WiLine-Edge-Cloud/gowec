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

func backupCmd(r *run) *cobra.Command {
	offering := group("offering", "Kinds of backup a VM can be put on", []string{"offerings"},
		backupOfferingListCmd(r))
	schedule := group("schedule", "VM backups on a schedule", []string{"schedules"},
		backupScheduleListCmd(r), backupScheduleCreateCmd(r), backupScheduleDeleteCmd(r))
	return group("backup", "VM backups and schedules", []string{"backups"},
		backupListCmd(r), backupCreateCmd(r), backupDeleteCmd(r), backupRestoreCmd(r),
		backupEnableCmd(r), backupDisableCmd(r), offering, schedule)
}

func backupArg(r *run) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return r.completeNames("backup", func(ctx context.Context, c *wec.Client) ([]string, error) {
		backups, err := c.ListBackups(ctx, "", wec.ListOptions{})
		names := make([]string, 0, len(backups))
		for _, b := range backups {
			if b.Name != "" {
				names = append(names, b.Name)
			}
		}
		return names, err
	})
}

func backupListCmd(r *run) *cobra.Command {
	var machine, status string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List backups",
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
			backups, err := c.ListBackups(cmd.Context(), machineID, wec.ListOptions{Status: status})
			if err != nil {
				return err
			}
			ids := make([]string, len(backups))
			for i, b := range backups {
				ids[i] = b.ID
			}
			return r.show(backups, ids, func() string {
				if len(backups) == 0 {
					return ui.Dim.Render("No backups.") + "\n"
				}
				now := time.Now()
				rows := make([][]string, len(backups))
				for i, b := range backups {
					kind := strings.ToLower(b.IntervalType)
					if kind == "" {
						kind = strings.ToLower(b.Type)
					}
					size := storageSize(b.Size)
					if b.VirtualSize > 0 {
						size += "\n" + ui.Dim.Render("of "+storageSize(b.VirtualSize))
					}
					rows[i] = []string{dash(b.MachineName) + "\n" + ui.Dim.Render(kind), ui.State(b.Status),
						size, created(b.Created, now), ui.Dim.Render(b.ID)}
				}
				return ui.RowTable([]string{"VM", "STATUS", "SIZE", "TAKEN", "ID"}, rows)
			})
		},
	}
	cmd.Flags().StringVar(&machine, "vm", "", "only this VM's backups (name or id)")
	cmd.Flags().StringVar(&status, "status", "", "only backups in this state (backedup, …)")
	storageMachineFlag(r, cmd)
	return cmd
}

func backupCreateCmd(r *run) *cobra.Command {
	var name, description string
	cmd := &cobra.Command{
		Use:               "create <vm>",
		Aliases:           []string{"new", "take"},
		Short:             "Back a VM up now (a VM keeps one taken by hand; schedules keep their own)",
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
			accepted, err := c.TakeBackup(cmd.Context(), m.ID, name, description, r.idempotencyKey())
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.waitJob(cmd.Context(), c, accepted, "Backing up "+m.Name, m.Name+" is backed up")
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "the backup's name")
	cmd.Flags().StringVar(&description, "description", "", "a note on what it holds")
	return cmd
}

// findBackup reads one backup by name or id, to say whose it is.
func findBackup(ctx context.Context, c *wec.Client, backup string) (*wec.Backup, error) {
	id, err := c.ResolveBackupID(ctx, backup)
	if err != nil {
		return nil, err
	}
	backups, err := c.ListBackups(ctx, "", wec.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range backups {
		if backups[i].ID == id {
			return &backups[i], nil
		}
	}
	return nil, &wec.APIError{Status: 404, Code: "unknown_backup", Message: "No backup has the id " + id}
}

func backupDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "delete <backup>",
		Aliases:           []string{"rm"},
		Short:             "Delete a backup (by name or id)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: backupArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			b, err := findBackup(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Delete this backup of %s, taken %s?", b.MachineName, day(b.Created)),
				"deleting can't be undone"); err != nil {
				return err
			}
			accepted, err := c.DeleteBackup(cmd.Context(), b.ID)
			if err != nil {
				return err
			}
			r.app.ForgetNames()
			return r.storageJob(cmd.Context(), c, accepted, "Deleting the backup", "The backup is deleted")
		},
	}
}

func backupRestoreCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "restore <backup>",
		Short:             "Restore a stopped VM from a backup (its disks are overwritten)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: backupArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			b, err := findBackup(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			if err := r.confirm(fmt.Sprintf("Restore %s from its backup of %s? Its disks are overwritten.",
				b.MachineName, day(b.Created)), "restoring overwrites the VM's disks"); err != nil {
				return err
			}
			accepted, err := c.RestoreBackup(cmd.Context(), b.ID)
			if err != nil {
				return err
			}
			return r.storageJob(cmd.Context(), c, accepted, "Restoring "+b.MachineName, b.MachineName+" is restored")
		},
	}
}

func backupEnableCmd(r *run) *cobra.Command {
	var offering string
	cmd := &cobra.Command{
		Use:               "enable <vm> [--offering <offering>]",
		Aliases:           []string{"on"},
		Short:             "Turn a VM's backups on (prepaid: charges the wallet a month of backups, priced first)",
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
			o, err := pickBackupOffering(cmd.Context(), c, m.ZoneID, offering)
			if err != nil {
				return err
			}
			spin := ui.StartSpinner(r.app.Err, r.tty(), "Pricing…")
			quote, err := c.QuoteBackups(cmd.Context(), m.ID)
			spin.Stop()
			if err != nil {
				r.planAddonHint(err, m.Name, "backups", "backups are")
				return err
			}
			if err := r.confirm(fmt.Sprintf("Turn backups on for %s (%s)?%s", m.Name, o.Name, chargeNote(quote)),
				"turning backups on can charge the wallet"); err != nil {
				return err
			}
			accepted, err := c.EnableBackups(cmd.Context(), m.ID, o.ID, r.idempotencyKey())
			if err != nil {
				r.planAddonHint(err, m.Name, "backups", "backups are")
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Turning backups on for "+m.Name, m.Name+" is backed up on "+o.Name)
		},
	}
	cmd.Flags().StringVar(&offering, "offering", "", "backup offering, name or id (the zone's only one if left out)")
	return cmd
}

// pickBackupOffering is the zone's offering named want, or its only one.
func pickBackupOffering(ctx context.Context, c *wec.Client, zoneID, want string) (*wec.BackupOffering, error) {
	offerings, err := c.ListBackupOfferings(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	var names []string
	for i, o := range offerings {
		names = append(names, o.Name)
		if want != "" && (o.ID == want || strings.EqualFold(o.Name, want)) {
			return &offerings[i], nil
		}
	}
	switch {
	case want != "":
		return nil, usagef("no backup offering %q in the VM's zone; there is %s", want, strings.Join(names, ", "))
	case len(offerings) == 0:
		return nil, fmt.Errorf("the VM's zone has no backup offering")
	case len(offerings) > 1:
		return nil, usagef("the zone has several backup offerings; pick one with --offering: %s", strings.Join(names, ", "))
	}
	return &offerings[0], nil
}

func backupDisableCmd(r *run) *cobra.Command {
	var deleteBackups bool
	cmd := &cobra.Command{
		Use:               "disable <vm>",
		Aliases:           []string{"off"},
		Short:             "Turn a VM's backups off",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			question := "Turn backups off for " + args[0] + "? Its backups are kept."
			if deleteBackups {
				question = "Turn backups off for " + args[0] + " and delete its backups?"
			}
			if err := r.confirm(question, "turning backups off"); err != nil {
				return err
			}
			accepted, err := c.DisableBackups(cmd.Context(), args[0], deleteBackups)
			if err != nil {
				return err
			}
			return r.waitJob(cmd.Context(), c, accepted, "Turning backups off for "+args[0], args[0]+" is no longer backed up")
		},
	}
	cmd.Flags().BoolVar(&deleteBackups, "delete-backups", false, "also delete the VM's backups")
	return cmd
}

func backupOfferingListCmd(r *run) *cobra.Command {
	var zone string
	list := listCmd(r, "List backup offerings",
		func(cmd *cobra.Command, c *wec.Client) ([]wec.BackupOffering, error) {
			id, err := zoneID(cmd, c, zone)
			if err != nil {
				return nil, err
			}
			return c.ListBackupOfferings(cmd.Context(), id)
		},
		[]string{"NAME", "ZONE", "DESCRIPTION", "ID"},
		func(o wec.BackupOffering) []string {
			return []string{o.Name, zoneName(o.ZoneName), ui.Dim.Render(o.Description), ui.Dim.Render(o.ID)}
		},
		func(o wec.BackupOffering) string { return o.ID })
	zoneFlag(list, &zone)
	return list
}

func backupScheduleListCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:               "list <vm>",
		Aliases:           []string{"ls"},
		Short:             "List a VM's backup schedules",
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
			schedules, err := c.ListBackupSchedules(cmd.Context(), m.ID)
			if err != nil {
				return err
			}
			ids := make([]string, len(schedules))
			rows := make([][]string, len(schedules))
			for i, s := range schedules {
				ids[i] = s.ID
				keeps := dash("")
				if s.MaxBackups > 0 {
					keeps = fmt.Sprint(s.MaxBackups)
				}
				rows[i] = []string{storageSchedule(s.IntervalType, s.Schedule), dash(s.Timezone), keeps, ui.Dim.Render(s.ID)}
			}
			return r.show(schedules, ids, func() string {
				if len(schedules) == 0 {
					return ui.Dim.Render(m.Name+" has no backup schedule.") + "\n"
				}
				return ui.Table([]string{"WHEN", "TIMEZONE", "KEEPS", "ID"}, rows)
			})
		},
	}
}

func backupScheduleCreateCmd(r *run) *cobra.Command {
	var body wec.CreateBackupSchedule
	var when storageWhen
	cmd := &cobra.Command{
		Use:               "create <vm> --interval <interval> --at <HH:MM> [--day <day>]",
		Aliases:           []string{"new"},
		Short:             "Back a VM up on a schedule",
		Example:           "  gowec backup schedule create web-1 --interval daily --at 03:00 --keep 2",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: machineArg(r),
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
			m, err := c.GetMachine(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			body.MachineID = m.ID
			s, err := c.CreateBackupSchedule(cmd.Context(), body)
			if err != nil {
				return err
			}
			return r.show(s, []string{s.ID}, func() string {
				return ui.Success(fmt.Sprintf("%s is backed up %s %s", m.Name,
					storageSchedule(s.IntervalType, s.Schedule), s.Timezone)) + "\n"
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(&body.IntervalType, "interval", "", "hourly, daily, weekly or monthly")
	when.flags(cmd)
	f.StringVar(&body.Timezone, "timezone", "UTC", "the schedule's timezone, e.g. America/Los_Angeles")
	f.IntVar(&body.MaxBackups, "keep", 0, "how many backups to keep (the offering's default if left out)")
	_ = cmd.MarkFlagRequired("interval")
	_ = cmd.RegisterFlagCompletionFunc("interval", fixed("hourly", "daily", "weekly", "monthly"))
	return cmd
}

func backupScheduleDeleteCmd(r *run) *cobra.Command {
	return &cobra.Command{
		Use:     "delete <schedule-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a backup schedule (the backups it took stay)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := r.client()
			if err != nil {
				return err
			}
			if err := r.confirm("Delete this backup schedule? The backups it took stay.",
				"deleting a schedule"); err != nil {
				return err
			}
			if err := c.DeleteBackupSchedule(cmd.Context(), args[0]); err != nil {
				return err
			}
			return r.show(map[string]string{"id": args[0], "status": "deleted"}, []string{args[0]},
				func() string { return ui.Success("The schedule is deleted") + "\n" })
		},
	}
}
