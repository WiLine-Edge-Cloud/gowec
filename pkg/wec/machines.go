package wec

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Machine is a virtual machine as the public API shows it.
type Machine struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	DisplayName         string            `json:"display_name,omitempty"`
	State               string            `json:"state"`
	ZoneID              string            `json:"zone_id,omitempty"`
	ZoneName            string            `json:"zone_name,omitempty"`
	TemplateID          string            `json:"template_id,omitempty"`
	TemplateName        string            `json:"template_name,omitempty"`
	OSName              string            `json:"os_name,omitempty"`
	ServiceOfferingID   string            `json:"service_offering_id,omitempty"`
	ServiceOfferingName string            `json:"service_offering_name,omitempty"`
	CPUCount            int               `json:"cpu_count,omitempty"`
	MemoryMB            int               `json:"memory_mb,omitempty"`
	CPUSpeedMHz         int               `json:"cpu_speed_mhz,omitempty"`
	RootDiskGB          int               `json:"root_disk_gb,omitempty"`
	PublicIP            string            `json:"public_ip,omitempty"`
	Addresses           []MachineAddress  `json:"addresses,omitempty"`
	BackupOfferingID    string            `json:"backup_offering_id,omitempty"`
	BackupOfferingName  string            `json:"backup_offering_name,omitempty"`
	DeleteProtection    bool              `json:"delete_protection"`
	Tags                map[string]string `json:"tags,omitempty"`
	Created             string            `json:"created,omitempty"`
}

// MachineAddress is one of a machine's network interfaces.
type MachineAddress struct {
	NetworkID   string `json:"network_id"`
	NetworkName string `json:"network_name"`
	IPAddress   string `json:"ip_address"`
	IsDefault   bool   `json:"is_default"`
}

// JobAccepted is the answer to an action CloudStack carries out in the background.
type JobAccepted struct {
	Status     string `json:"status"`
	JobID      string `json:"job_id"`
	ResourceID string `json:"resource_id,omitempty"`
}

func machinePath(machine string) string { return "/machines/" + PathEscape(machine) }

// ListMachines returns every machine matching opts.
func (c *Client) ListMachines(ctx context.Context, opts ListOptions) ([]Machine, error) {
	return listAll[Machine](ctx, c, "/machines", opts)
}

// GetMachine reads one machine by name or id.
func (c *Client) GetMachine(ctx context.Context, machine string) (*Machine, error) {
	var m Machine
	return &m, c.get(ctx, machinePath(machine), nil, &m)
}

func (c *Client) machineAction(ctx context.Context, method, path string, query url.Values, body any, idemKey string) (*JobAccepted, error) {
	var job JobAccepted
	err := c.Do(ctx, method, path, body, &job, &RequestOptions{Query: query, IdempotencyKey: idemKey})
	return &job, err
}

// StartMachine starts a stopped machine.
func (c *Client) StartMachine(ctx context.Context, machine string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, machinePath(machine)+"/start", nil, nil, "")
}

// StopMachine stops a machine; forced pulls the plug.
func (c *Client) StopMachine(ctx context.Context, machine string, forced bool) (*JobAccepted, error) {
	var q url.Values
	if forced {
		q = url.Values{"forced": {strconv.FormatBool(forced)}}
	}
	return c.machineAction(ctx, http.MethodPost, machinePath(machine)+"/stop", q, nil, "")
}

// RebootMachine reboots a running machine.
func (c *Client) RebootMachine(ctx context.Context, machine string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, machinePath(machine)+"/reboot", nil, nil, "")
}

// ResizeMachine changes a machine's size (name or id). It can spend, so it
// needs an idempotency key.
func (c *Client) ResizeMachine(ctx context.Context, machine, size, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, machinePath(machine)+"/resize", nil,
		map[string]string{"size": size}, idemKey)
}

// RenameMachine changes a machine's name.
func (c *Client) RenameMachine(ctx context.Context, machine, name string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPatch, machinePath(machine), nil,
		map[string]string{"name": name}, "")
}

// DeleteMachine deletes a machine. A prepaid machine on a plan is refused
// with use_plan_delete: delete its plan instead (DeletePlanNow).
func (c *Client) DeleteMachine(ctx context.Context, machine string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, machinePath(machine), nil, nil, "")
}

// MachineUsage is one machine's CPU over the window, plus its latest readings.
type MachineUsage struct {
	CPU    []*float64 `json:"cpu"` // average % per bucket, oldest first; nil where no sample
	CPUNow *float64   `json:"cpu_now"`
	MemNow *float64   `json:"mem_now"`
}

// MachinesUsage is the use of several machines over the last WindowHours.
type MachinesUsage struct {
	WindowHours int                     `json:"window_hours"`
	Buckets     int                     `json:"buckets"`
	Machines    map[string]MachineUsage `json:"machines"`
}

// GetMachinesUsage reads the CPU and memory use of machines (by id), 50 at a time.
func (c *Client) GetMachinesUsage(ctx context.Context, ids []string) (*MachinesUsage, error) {
	all := &MachinesUsage{Machines: map[string]MachineUsage{}}
	for start := 0; start < len(ids); start += 50 {
		end := min(start+50, len(ids))
		var part MachinesUsage
		q := url.Values{"ids": {strings.Join(ids[start:end], ",")}}
		if err := c.get(ctx, "/usage/machines", q, &part); err != nil {
			return nil, err
		}
		all.WindowHours, all.Buckets = part.WindowHours, part.Buckets
		for id, u := range part.Machines {
			all.Machines[id] = u
		}
	}
	return all, nil
}
