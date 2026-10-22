package wec

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Volume is a disk: a machine's root disk or a data volume.
type Volume struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Type             string  `json:"type,omitempty"`
	State            string  `json:"state,omitempty"`
	SizeGB           float64 `json:"size_gb,omitempty"`
	ZoneID           string  `json:"zone_id,omitempty"`
	ZoneName         string  `json:"zone_name,omitempty"`
	DiskOfferingID   string  `json:"disk_offering_id,omitempty"`
	DiskOfferingName string  `json:"disk_offering_name,omitempty"`
	MachineID        string  `json:"machine_id,omitempty"`
	MachineName      string  `json:"machine_name,omitempty"`
	Created          string  `json:"created,omitempty"`
}

// ListVolumes returns every volume matching opts.
func (c *Client) ListVolumes(ctx context.Context, opts ListOptions) ([]Volume, error) {
	return listAll[Volume](ctx, c, "/volumes", opts)
}

// GetVolume reads one volume by id.
func (c *Client) GetVolume(ctx context.Context, volumeID string) (*Volume, error) {
	var v Volume
	return &v, c.get(ctx, "/volumes/"+PathEscape(volumeID), nil, &v)
}

// ResolveVolumeID turns a volume name or id into its id.
func (c *Client) ResolveVolumeID(ctx context.Context, volume string) (string, error) {
	if IsID(volume) {
		return volume, nil
	}
	vols, err := c.ListVolumes(ctx, ListOptions{Name: volume})
	if err != nil {
		return "", err
	}
	return resolve(vols, volume, "volume", func(v Volume) (string, string) { return v.ID, v.Name })
}

// AttachVolume attaches a volume (id) to a machine (id).
func (c *Client) AttachVolume(ctx context.Context, volumeID, machineID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/volumes/"+PathEscape(volumeID)+"/attach", nil,
		map[string]string{"machine_id": machineID}, "")
}

// DetachVolume detaches a volume (id) from its machine.
func (c *Client) DetachVolume(ctx context.Context, volumeID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/volumes/"+PathEscape(volumeID)+"/detach", nil, nil, "")
}

// FindDiskOffering finds a disk offering by name or id, optionally in one zone (id).
func (c *Client) FindDiskOffering(ctx context.Context, offering, zoneID string) (*Disk, error) {
	disks, err := c.ListDisks(ctx, zoneID)
	if err != nil {
		return nil, err
	}
	id, err := resolve(disks, offering, "disk_offering", func(d Disk) (string, string) { return d.ID, d.Name })
	if err != nil {
		return nil, err
	}
	for i := range disks {
		if disks[i].ID == id {
			return &disks[i], nil
		}
	}
	return &Disk{ID: id}, nil
}

// CreateVolume is the body of POST /volumes. Zone, offering and machine are ids.
type CreateVolume struct {
	Name           string `json:"name"`
	ZoneID         string `json:"zone_id"`
	DiskOfferingID string `json:"disk_offering_id"`
	SizeGB         int    `json:"size_gb,omitempty"`
	MachineID      string `json:"machine_id,omitempty"`
}

// CreateVolume makes a data volume (postpaid accounts; a prepaid account is
// refused with use_volume_plan). It can spend, so it needs an idempotency key.
func (c *Client) CreateVolume(ctx context.Context, body CreateVolume, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/volumes", nil, body, idemKey)
}

// BuyVolume is the body of POST /volumes/plans: a data volume on a plan of
// its own. QuotedTotal must be the term's price, or the call is refused with
// plan_price_changed and the current one.
type BuyVolume struct {
	Name             string  `json:"name"`
	ZoneID           string  `json:"zone_id"`
	DiskOfferingID   string  `json:"disk_offering_id"`
	SizeGB           int     `json:"size_gb,omitempty"`
	TermMonths       int     `json:"term_months"`
	AutoRenew        bool    `json:"auto_renew"`
	VirtualMachineID string  `json:"virtual_machine_id,omitempty"`
	QuotedTotal      float64 `json:"quoted_total"`
}

// BuyVolumePlan buys a volume's plan from the wallet, then makes the volume.
// It waits for CloudStack; the answer (volume_id, charged, attached,
// finishing, …) is the portal's, kept as sent.
func (c *Client) BuyVolumePlan(ctx context.Context, body BuyVolume, idemKey string) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, http.MethodPost, "/volumes/plans", body, &out, &RequestOptions{IdempotencyKey: idemKey})
	return out, err
}

// ResizeVolume grows a volume (id) to sizeGB or to another offering (id).
// It can spend, so it needs an idempotency key.
func (c *Client) ResizeVolume(ctx context.Context, volumeID string, sizeGB int, offeringID, idemKey string) (*JobAccepted, error) {
	body := map[string]any{}
	if sizeGB > 0 {
		body["size_gb"] = sizeGB
	}
	if offeringID != "" {
		body["disk_offering_id"] = offeringID
	}
	return c.machineAction(ctx, http.MethodPost, "/volumes/"+PathEscape(volumeID)+"/resize", nil, body, idemKey)
}

// DeleteVolume deletes a detached volume (id). A volume on a plan is refused
// with use_volume_plan.
func (c *Client) DeleteVolume(ctx context.Context, volumeID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, "/volumes/"+PathEscape(volumeID), nil, nil, "")
}

// VolumeQuote is what a data volume of one size costs on a plan, per term.
type VolumeQuote struct {
	SizeGB         float64 `json:"size_gb"`
	StorageGBMonth float64 `json:"storage_gb_month"`
	Terms          []struct {
		TermMonths int     `json:"term_months"`
		Total      float64 `json:"total"`
		Monthly    float64 `json:"monthly"`
	} `json:"terms"`
}

// Total is the price of the term of the given length.
func (q *VolumeQuote) Total(months int) (float64, error) {
	for _, t := range q.Terms {
		if t.TermMonths == months {
			return t.Total, nil
		}
	}
	return 0, fmt.Errorf("no %d-month term for this volume", months)
}

// QuoteVolumePlan prices a data volume on a plan: by its disk offering (id),
// and its size for a custom-size offering.
func (c *Client) QuoteVolumePlan(ctx context.Context, offeringID string, sizeGB int) (*VolumeQuote, error) {
	q := url.Values{"disk_offering_id": {offeringID}}
	if sizeGB > 0 {
		q.Set("size_gb", strconv.Itoa(sizeGB))
	}
	var out VolumeQuote
	return &out, c.get(ctx, "/volumes/quote", q, &out)
}
