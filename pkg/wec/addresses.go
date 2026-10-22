package wec

import (
	"context"
	"net/http"
)

// Address is a public IP address.
type Address struct {
	ID                   string `json:"id"`
	IPAddress            string `json:"ip_address"`
	State                string `json:"state,omitempty"`
	ZoneID               string `json:"zone_id,omitempty"`
	ZoneName             string `json:"zone_name,omitempty"`
	VPCID                string `json:"vpc_id,omitempty"`
	VPCName              string `json:"vpc_name,omitempty"`
	IsSourceNAT          bool   `json:"is_source_nat"`
	StaticNATMachineID   string `json:"static_nat_machine_id,omitempty"`
	StaticNATMachineName string `json:"static_nat_machine_name,omitempty"`
}

// ListAddresses returns every public address matching opts (name = the IP).
func (c *Client) ListAddresses(ctx context.Context, opts ListOptions) ([]Address, error) {
	return listAll[Address](ctx, c, "/addresses", opts)
}

// FindAddress reads one public address by its IP or its id.
func (c *Client) FindAddress(ctx context.Context, address string) (*Address, error) {
	all, err := c.ListAddresses(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	return findRef(all, address, "address", func(a Address) (string, string) { return a.ID, a.IPAddress })
}

func addressPath(addressID string) string { return "/addresses/" + PathEscape(addressID) }

// AllocateAddress takes a new public address in a VPC (id), for a machine
// (id, optional). It can charge the wallet, so it needs an idempotency key.
func (c *Client) AllocateAddress(ctx context.Context, vpcID, machineID, idemKey string) (*JobAccepted, error) {
	body := map[string]string{"vpc_id": vpcID}
	if machineID != "" {
		body["machine_id"] = machineID
	}
	return c.machineAction(ctx, http.MethodPost, "/addresses", nil, body, idemKey)
}

// ReleaseAddress gives a public address (id) back.
func (c *Client) ReleaseAddress(ctx context.Context, addressID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, addressPath(addressID), nil, nil, "")
}

// EnableStaticNAT points a public address (id) at a machine (id), through
// one of its networks (id, optional).
func (c *Client) EnableStaticNAT(ctx context.Context, addressID, machineID, networkID string) (*JobAccepted, error) {
	body := map[string]string{"machine_id": machineID}
	if networkID != "" {
		body["network_id"] = networkID
	}
	return c.machineAction(ctx, http.MethodPost, addressPath(addressID)+"/static-nat", nil, body, "")
}

// DisableStaticNAT stops pointing a public address (id) at its machine.
func (c *Client) DisableStaticNAT(ctx context.Context, addressID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, addressPath(addressID)+"/static-nat", nil, nil, "")
}
