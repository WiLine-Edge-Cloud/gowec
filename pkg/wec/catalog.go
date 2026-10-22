package wec

import (
	"context"
	"net/url"
)

// Zone is a place machines run.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Size is a machine size (a service offering).
type Size struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayText string `json:"display_text,omitempty"`
	CPUCount    int    `json:"cpu_count"`
	CPUSpeedMHz int    `json:"cpu_speed_mhz,omitempty"`
	MemoryMB    int    `json:"memory_mb"`
}

// Template is an operating system image.
type Template struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DisplayText     string `json:"display_text,omitempty"`
	OSType          string `json:"os_type,omitempty"`
	ZoneID          string `json:"zone_id,omitempty"`
	ZoneName        string `json:"zone_name,omitempty"`
	SizeBytes       int64  `json:"size_bytes,omitempty"`
	PasswordEnabled bool   `json:"password_enabled"`
}

// Disk is a root disk size a machine can launch with.
type Disk struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DisplayText string  `json:"display_text,omitempty"`
	SizeGB      float64 `json:"size_gb"`
	CustomSize  bool    `json:"custom_size"`
}

func zoneQuery(zone string) ListOptions {
	if zone == "" {
		return ListOptions{}
	}
	return ListOptions{Extra: url.Values{"zone_id": {zone}}}
}

// ListZones returns the zones.
func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	return listAll[Zone](ctx, c, "/zones", ListOptions{})
}

// ListSizes returns the machine sizes, optionally for one zone (id).
func (c *Client) ListSizes(ctx context.Context, zoneID string) ([]Size, error) {
	return listAll[Size](ctx, c, "/sizes", zoneQuery(zoneID))
}

// ListTemplates returns the operating systems, optionally for one zone (id).
func (c *Client) ListTemplates(ctx context.Context, zoneID string) ([]Template, error) {
	return listAll[Template](ctx, c, "/templates", zoneQuery(zoneID))
}

// ListDisks returns the root disk sizes, optionally for one zone (id).
func (c *Client) ListDisks(ctx context.Context, zoneID string) ([]Disk, error) {
	return listAll[Disk](ctx, c, "/disks", zoneQuery(zoneID))
}

// ResolveZoneID turns a zone name or id into its id.
func (c *Client) ResolveZoneID(ctx context.Context, zone string) (string, error) {
	zones, err := c.ListZones(ctx)
	if err != nil {
		return "", err
	}
	return resolve(zones, zone, "zone", func(z Zone) (string, string) { return z.ID, z.Name })
}
