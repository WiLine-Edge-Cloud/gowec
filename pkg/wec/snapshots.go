package wec

import (
	"context"
	"net/http"
	"net/url"
)

// Snapshot is a volume snapshot.
type Snapshot struct {
	ID           string `json:"id"`
	Name         string `json:"name,omitempty"`
	State        string `json:"state,omitempty"`
	Type         string `json:"type,omitempty"`
	IntervalType string `json:"interval_type,omitempty"`
	VolumeID     string `json:"volume_id,omitempty"`
	VolumeName   string `json:"volume_name,omitempty"`
	ZoneID       string `json:"zone_id,omitempty"`
	ZoneName     string `json:"zone_name,omitempty"`
	PhysicalSize int64  `json:"physical_size,omitempty"`
	Created      string `json:"created,omitempty"`
}

// MachineSnapshot is a snapshot of a whole machine.
type MachineSnapshot struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
	Type        string `json:"type,omitempty"`
	Current     bool   `json:"current"`
	MachineID   string `json:"machine_id,omitempty"`
	MachineName string `json:"machine_name,omitempty"`
	Created     string `json:"created,omitempty"`
}

// Label is the name a person gave the snapshot, else CloudStack's.
func (s MachineSnapshot) Label() string {
	if s.DisplayName != "" {
		return s.DisplayName
	}
	return s.Name
}

// SnapshotPolicy snapshots a volume on a schedule.
type SnapshotPolicy struct {
	ID           string `json:"id"`
	VolumeID     string `json:"volume_id,omitempty"`
	IntervalType string `json:"interval_type,omitempty"`
	Schedule     string `json:"schedule,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	MaxSnapshots int    `json:"max_snapshots,omitempty"`
	ForDisplay   *bool  `json:"for_display,omitempty"`
}

func storageFilter(key, id string) url.Values {
	if id == "" {
		return nil
	}
	return url.Values{key: {id}}
}

// ListSnapshots returns the volume snapshots matching opts, optionally of one volume (id).
func (c *Client) ListSnapshots(ctx context.Context, volumeID string, opts ListOptions) ([]Snapshot, error) {
	opts.Extra = storageFilter("volume_id", volumeID)
	return listAll[Snapshot](ctx, c, "/snapshots", opts)
}

// ResolveSnapshotID turns a volume snapshot's name or id into its id.
func (c *Client) ResolveSnapshotID(ctx context.Context, snapshot string) (string, error) {
	if IsID(snapshot) {
		return snapshot, nil
	}
	snaps, err := c.ListSnapshots(ctx, "", ListOptions{Name: snapshot})
	if err != nil {
		return "", err
	}
	return resolve(snaps, snapshot, "snapshot", func(s Snapshot) (string, string) { return s.ID, s.Name })
}

// CreateSnapshot snapshots a volume (id). It needs an idempotency key.
func (c *Client) CreateSnapshot(ctx context.Context, volumeID, name, idemKey string) (*JobAccepted, error) {
	body := map[string]string{"volume_id": volumeID}
	if name != "" {
		body["name"] = name
	}
	return c.machineAction(ctx, http.MethodPost, "/snapshots", nil, body, idemKey)
}

// DeleteSnapshot deletes a volume snapshot (id).
func (c *Client) DeleteSnapshot(ctx context.Context, snapshotID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/snapshots/"+PathEscape(snapshotID), nil, nil, "")
}

// RevertSnapshot puts a volume back to a snapshot (id); what was written since is lost.
func (c *Client) RevertSnapshot(ctx context.Context, snapshotID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/snapshots/"+PathEscape(snapshotID)+"/revert", nil, nil, "")
}

// ListMachineSnapshots returns the machine snapshots, optionally of one machine (id).
func (c *Client) ListMachineSnapshots(ctx context.Context, machineID string, opts ListOptions) ([]MachineSnapshot, error) {
	opts.Extra = storageFilter("machine_id", machineID)
	return listAll[MachineSnapshot](ctx, c, "/machine-snapshots", opts)
}

// ResolveMachineSnapshotID turns a machine snapshot's name or id into its id.
// The API's name filter sees only CloudStack's name, so this lists them all
// and matches the name a person gave first.
func (c *Client) ResolveMachineSnapshotID(ctx context.Context, snapshot string) (string, error) {
	if IsID(snapshot) {
		return snapshot, nil
	}
	snaps, err := c.ListMachineSnapshots(ctx, "", ListOptions{})
	if err != nil {
		return "", err
	}
	id, err := resolve(snaps, snapshot, "machine_snapshot", func(s MachineSnapshot) (string, string) { return s.ID, s.Label() })
	if IsCode(err, "unknown_machine_snapshot") {
		if byName, err2 := resolve(snaps, snapshot, "machine_snapshot", func(s MachineSnapshot) (string, string) { return s.ID, s.Name }); err2 == nil {
			return byName, nil
		}
	}
	return id, err
}

// CreateMachineSnapshot is the body of POST /machine-snapshots.
type CreateMachineSnapshot struct {
	MachineID     string `json:"machine_id"`
	Name          string `json:"name,omitempty"`
	Description   string `json:"description,omitempty"`
	IncludeMemory *bool  `json:"include_memory,omitempty"`
}

// CreateMachineSnapshot snapshots a machine. It needs an idempotency key.
func (c *Client) CreateMachineSnapshot(ctx context.Context, body CreateMachineSnapshot, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/machine-snapshots", nil, body, idemKey)
}

// DeleteMachineSnapshot deletes a machine snapshot (id).
func (c *Client) DeleteMachineSnapshot(ctx context.Context, snapshotID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/machine-snapshots/"+PathEscape(snapshotID), nil, nil, "")
}

// RevertMachineSnapshot puts a machine back to a snapshot (id); what changed since is lost.
func (c *Client) RevertMachineSnapshot(ctx context.Context, snapshotID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/machine-snapshots/"+PathEscape(snapshotID)+"/revert", nil, nil, "")
}

// ListSnapshotPolicies returns the snapshot schedules, optionally of one volume (id).
func (c *Client) ListSnapshotPolicies(ctx context.Context, volumeID string) ([]SnapshotPolicy, error) {
	return listAll[SnapshotPolicy](ctx, c, "/snapshot-policies", ListOptions{Extra: storageFilter("volume_id", volumeID)})
}

// CreateSnapshotPolicy is the body of POST /snapshot-policies.
type CreateSnapshotPolicy struct {
	VolumeID     string `json:"volume_id"`
	IntervalType string `json:"interval_type"`
	Schedule     string `json:"schedule"`
	Timezone     string `json:"timezone"`
	MaxSnapshots int    `json:"max_snapshots"`
}

// CreateSnapshotPolicy snapshots a volume on a schedule.
func (c *Client) CreateSnapshotPolicy(ctx context.Context, body CreateSnapshotPolicy) (*SnapshotPolicy, error) {
	var p SnapshotPolicy
	return &p, c.Do(ctx, http.MethodPost, "/snapshot-policies", body, &p, nil)
}

// UpdateSnapshotPolicy shows or hides a snapshot policy (id) in the dashboard.
func (c *Client) UpdateSnapshotPolicy(ctx context.Context, policyID string, forDisplay bool) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPatch, "/snapshot-policies/"+PathEscape(policyID), nil,
		map[string]bool{"for_display": forDisplay}, "")
}

// DeleteSnapshotPolicy stops a snapshot schedule (id); the snapshots it took stay.
func (c *Client) DeleteSnapshotPolicy(ctx context.Context, policyID string) error {
	return c.Do(ctx, http.MethodDelete, "/snapshot-policies/"+PathEscape(policyID), nil, nil, nil)
}
