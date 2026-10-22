package wec

import (
	"context"
	"net/http"
	"net/url"
)

// BackupOffering is a kind of backup a machine can be put on.
type BackupOffering struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	ZoneID      string `json:"zone_id,omitempty"`
	ZoneName    string `json:"zone_name,omitempty"`
}

// Backup is one backup of a machine.
type Backup struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	Status             string `json:"status,omitempty"`
	Type               string `json:"type,omitempty"`
	IntervalType       string `json:"interval_type,omitempty"`
	MachineID          string `json:"machine_id,omitempty"`
	MachineName        string `json:"machine_name,omitempty"`
	BackupOfferingID   string `json:"backup_offering_id,omitempty"`
	BackupOfferingName string `json:"backup_offering_name,omitempty"`
	ZoneID             string `json:"zone_id,omitempty"`
	ZoneName           string `json:"zone_name,omitempty"`
	Size               int64  `json:"size,omitempty"`
	VirtualSize        int64  `json:"virtual_size,omitempty"`
	Created            string `json:"created,omitempty"`
}

// BackupSchedule backs a machine up on a schedule.
type BackupSchedule struct {
	ID           string `json:"id"`
	MachineID    string `json:"machine_id,omitempty"`
	IntervalType string `json:"interval_type,omitempty"`
	Schedule     string `json:"schedule,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	MaxBackups   int    `json:"max_backups,omitempty"`
}

// ListBackupOfferings returns the backup offerings, optionally of one zone (id).
func (c *Client) ListBackupOfferings(ctx context.Context, zoneID string) ([]BackupOffering, error) {
	return listAll[BackupOffering](ctx, c, "/backup-offerings", zoneQuery(zoneID))
}

// ListBackups returns the backups matching opts, optionally of one machine (id).
func (c *Client) ListBackups(ctx context.Context, machineID string, opts ListOptions) ([]Backup, error) {
	opts.Extra = storageFilter("machine_id", machineID)
	return listAll[Backup](ctx, c, "/backups", opts)
}

// ResolveBackupID turns a backup's name or id into its id.
func (c *Client) ResolveBackupID(ctx context.Context, backup string) (string, error) {
	if IsID(backup) {
		return backup, nil
	}
	backups, err := c.ListBackups(ctx, "", ListOptions{Name: backup})
	if err != nil {
		return "", err
	}
	return resolve(backups, backup, "backup", func(b Backup) (string, string) { return b.ID, b.Name })
}

// TakeBackup backs a machine (id) up now. It needs an idempotency key.
func (c *Client) TakeBackup(ctx context.Context, machineID, name, description, idemKey string) (*JobAccepted, error) {
	body := map[string]string{"machine_id": machineID}
	if name != "" {
		body["name"] = name
	}
	if description != "" {
		body["description"] = description
	}
	return c.machineAction(ctx, http.MethodPost, "/backups", nil, body, idemKey)
}

// DeleteBackup deletes a backup (id).
func (c *Client) DeleteBackup(ctx context.Context, backupID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/backups/"+PathEscape(backupID), nil, nil, "")
}

// RestoreBackup restores a stopped machine from a backup (id); its disks are overwritten.
func (c *Client) RestoreBackup(ctx context.Context, backupID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/backups/"+PathEscape(backupID)+"/restore", nil, nil, "")
}

// ListBackupSchedules returns a machine's (id) backup schedules.
func (c *Client) ListBackupSchedules(ctx context.Context, machineID string) ([]BackupSchedule, error) {
	return listAll[BackupSchedule](ctx, c, "/backup-schedules", ListOptions{Extra: storageFilter("machine_id", machineID)})
}

// CreateBackupSchedule is the body of POST /backup-schedules.
type CreateBackupSchedule struct {
	MachineID    string `json:"machine_id"`
	IntervalType string `json:"interval_type"`
	Schedule     string `json:"schedule"`
	Timezone     string `json:"timezone"`
	MaxBackups   int    `json:"max_backups,omitempty"`
}

// CreateBackupSchedule backs a machine up on a schedule.
func (c *Client) CreateBackupSchedule(ctx context.Context, body CreateBackupSchedule) (*BackupSchedule, error) {
	var s BackupSchedule
	return &s, c.Do(ctx, http.MethodPost, "/backup-schedules", body, &s, nil)
}

// DeleteBackupSchedule deletes a backup schedule (id); the backups it took stay.
func (c *Client) DeleteBackupSchedule(ctx context.Context, scheduleID string) error {
	return c.Do(ctx, http.MethodDelete, "/backup-schedules/"+PathEscape(scheduleID), nil, nil, nil)
}

// EnableBackups puts a machine (name or id) on a backup offering (id). On
// prepaid it charges the wallet, so it needs an idempotency key; a machine on
// a plan is refused with use_plan_addon.
func (c *Client) EnableBackups(ctx context.Context, machine, offeringID, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, machinePath(machine)+"/backup-offering", nil,
		map[string]string{"backup_offering_id": offeringID}, idemKey)
}

// DisableBackups takes a machine (name or id) off its backup offering;
// deleteBackups also deletes the backups it has.
func (c *Client) DisableBackups(ctx context.Context, machine string, deleteBackups bool) (*JobAccepted, error) {
	var q url.Values
	if deleteBackups {
		q = url.Values{"forced": {"true"}}
	}
	return c.machineAction(ctx, http.MethodDelete, machinePath(machine)+"/backup-offering", q, nil, "")
}
