package wec

import (
	"context"
	"net/http"
	"strings"
)

// Network is a network: a VPC's tier (subnet) or an isolated network.
type Network struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CIDR     string `json:"cidr,omitempty"`
	State    string `json:"state,omitempty"`
	ZoneID   string `json:"zone_id,omitempty"`
	ZoneName string `json:"zone_name,omitempty"`
	VPCID    string `json:"vpc_id,omitempty"`
	VPCName  string `json:"vpc_name,omitempty"`
}

// CreateNetwork is the body of POST /networks. Ids only.
type CreateNetwork struct {
	Name              string `json:"name"`
	ZoneID            string `json:"zone_id"`
	NetworkOfferingID string `json:"network_offering_id"`
	VPCID             string `json:"vpc_id,omitempty"`
	Gateway           string `json:"gateway,omitempty"`
	Netmask           string `json:"netmask,omitempty"`
	ACLID             string `json:"acl_id,omitempty"`
	DisplayText       string `json:"display_text,omitempty"`
}

// FirewallRule is one of a network's firewall rules.
type FirewallRule struct {
	ID        string `json:"id"`
	Protocol  string `json:"protocol,omitempty"`
	CIDR      string `json:"cidr,omitempty"`
	StartPort *int   `json:"start_port"`
	EndPort   *int   `json:"end_port"`
	ICMPType  *int   `json:"icmp_type"`
	ICMPCode  *int   `json:"icmp_code"`
	AddressID string `json:"address_id,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
	State     string `json:"state,omitempty"`
}

func networkPath(networkID string) string { return "/networks/" + PathEscape(networkID) }

// ListNetworks returns every network matching opts.
func (c *Client) ListNetworks(ctx context.Context, opts ListOptions) ([]Network, error) {
	return listAll[Network](ctx, c, "/networks", opts)
}

// FindNetwork reads one network by name or id (the API has no GET for one).
func (c *Client) FindNetwork(ctx context.Context, network string) (*Network, error) {
	all, err := c.ListNetworks(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	return findRef(all, network, "network", func(n Network) (string, string) { return n.ID, n.Name })
}

// CreateNetwork creates a network or a VPC tier. It needs an idempotency key.
func (c *Client) CreateNetwork(ctx context.Context, body CreateNetwork, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/networks", nil, body, idemKey)
}

// RenameNetwork changes a network's (id) name.
func (c *Client) RenameNetwork(ctx context.Context, networkID, name string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPatch, networkPath(networkID), nil, map[string]string{"name": name}, "")
}

// RestartNetwork has a network's (id) router apply its settings again.
func (c *Client) RestartNetwork(ctx context.Context, networkID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, networkPath(networkID)+"/restart", nil, nil, "")
}

// DeleteNetwork deletes a network (id); no machine may be on it.
func (c *Client) DeleteNetwork(ctx context.Context, networkID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, networkPath(networkID), nil, nil, "")
}

// ListNetworkFirewallRules returns a network's (id) firewall rules.
func (c *Client) ListNetworkFirewallRules(ctx context.Context, networkID string) ([]FirewallRule, error) {
	return listAll[FirewallRule](ctx, c, networkPath(networkID)+"/firewall-rules", ListOptions{})
}

// findRef is the one item ref names, by id or by name as resolve matches
// it. An id that isn't among items is unknown too.
func findRef[T any](items []T, ref, kind string, key func(T) (id, name string)) (*T, error) {
	id, err := resolve(items, ref, kind, key)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if itemID, _ := key(items[i]); strings.EqualFold(itemID, id) {
			return &items[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Code: "unknown_" + kind, Message: "No " + kind + " has the id " + ref}
}
