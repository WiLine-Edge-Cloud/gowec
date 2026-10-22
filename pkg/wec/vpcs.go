package wec

import (
	"context"
	"net/http"
)

// VPC is a virtual private cloud: an address range its networks share.
type VPC struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CIDR     string `json:"cidr,omitempty"`
	State    string `json:"state,omitempty"`
	ZoneID   string `json:"zone_id,omitempty"`
	ZoneName string `json:"zone_name,omitempty"`
}

// CreateVPC is the body of POST /vpcs. Ids only.
type CreateVPC struct {
	Name          string `json:"name"`
	ZoneID        string `json:"zone_id"`
	VPCOfferingID string `json:"vpc_offering_id"`
	CIDR          string `json:"cidr,omitempty"`
	DisplayText   string `json:"display_text,omitempty"`
}

func vpcPath(vpcID string) string { return "/vpcs/" + PathEscape(vpcID) }

// ListVPCs returns every VPC matching opts.
func (c *Client) ListVPCs(ctx context.Context, opts ListOptions) ([]VPC, error) {
	return listAll[VPC](ctx, c, "/vpcs", opts)
}

// FindVPC reads one VPC by name or id (the API has no GET for one).
func (c *Client) FindVPC(ctx context.Context, vpc string) (*VPC, error) {
	all, err := c.ListVPCs(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	return findRef(all, vpc, "vpc", func(v VPC) (string, string) { return v.ID, v.Name })
}

// CreateVPC creates a VPC. It needs an idempotency key.
func (c *Client) CreateVPC(ctx context.Context, body CreateVPC, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/vpcs", nil, body, idemKey)
}

// RenameVPC changes a VPC's (id) name.
func (c *Client) RenameVPC(ctx context.Context, vpcID, name string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPatch, vpcPath(vpcID), nil, map[string]string{"name": name}, "")
}

// RestartVPC restarts a VPC's (id) router.
func (c *Client) RestartVPC(ctx context.Context, vpcID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, vpcPath(vpcID)+"/restart", nil, nil, "")
}

// SetVPCSourceNAT makes ip (one of the VPC's addresses, the IP itself) the
// address the VPC's outbound traffic leaves from.
func (c *Client) SetVPCSourceNAT(ctx context.Context, vpcID, ip string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, vpcPath(vpcID)+"/source-nat", nil, map[string]string{"ip_address": ip}, "")
}

// DeleteVPC deletes a VPC (id) and its addresses. One holding a plan's
// address is refused with plan_addon_locked.
func (c *Client) DeleteVPC(ctx context.Context, vpcID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, vpcPath(vpcID), nil, nil, "")
}
