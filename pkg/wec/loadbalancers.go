package wec

import (
	"context"
	"net/http"
	"net/url"
)

// LoadBalancer spreads connections to one public address over machines on
// one of a VPC's subnets. Its ID is the ID of that address. State is ready,
// updating (a change is being applied) or deleting.
type LoadBalancer struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name,omitempty"`
	Address   string                 `json:"address,omitempty"`
	Created   string                 `json:"created,omitempty"`
	State     string                 `json:"state"`
	Subnet    LoadBalancerSubnet     `json:"subnet"`
	Listeners []LoadBalancerListener `json:"listeners"`
	Machines  []LoadBalancerMachine  `json:"machines"`
}

// LoadBalancerSubnet is the subnet a balancer's machines are on.
type LoadBalancerSubnet struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	CIDR string `json:"cidr,omitempty"`
}

// LoadBalancerListener passes one public port to one port on every machine.
// Protocol is tcp or tcp-proxy; Algorithm roundrobin, leastconn or source;
// Sticky none, source or cookie (set only when one balancer is read).
type LoadBalancerListener struct {
	ID          string `json:"id"`
	PublicPort  int    `json:"public_port"`
	PrivatePort int    `json:"private_port"`
	Protocol    string `json:"protocol"`
	Algorithm   string `json:"algorithm,omitempty"`
	Sticky      string `json:"sticky,omitempty"`
	State       string `json:"state,omitempty"`
}

// LoadBalancerMachine is a machine behind a balancer. State is the
// machine's own (Running, Stopped…), not a health check.
type LoadBalancerMachine struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	PrivateAddress string `json:"private_address,omitempty"`
	State          string `json:"state,omitempty"`
}

// ListenerSpec is a listener to create. Sticky "cookie" needs public port 80,
// which the VPC router always serves as HTTP.
type ListenerSpec struct {
	PublicPort  int    `json:"public_port"`
	PrivatePort int    `json:"private_port"`
	Protocol    string `json:"protocol,omitempty"`
	Algorithm   string `json:"algorithm,omitempty"`
	Sticky      string `json:"sticky,omitempty"`
}

// CreateLoadBalancer is the body of POST /vpcs/{id}/load-balancers.
type CreateLoadBalancer struct {
	Name       string         `json:"name"`
	SubnetID   string         `json:"subnet_id"`
	Listeners  []ListenerSpec `json:"listeners"`
	MachineIDs []string       `json:"machine_ids"`
}

// ListenerChanges is the body of PATCH …/listeners/{id}: empty fields are
// kept. Ports can't change; delete the listener and add another.
type ListenerChanges struct {
	Algorithm string `json:"algorithm,omitempty"`
	Sticky    string `json:"sticky,omitempty"`
}

// LoadBalancerAccepted answers a create: the new balancer's ID and the
// operation building it (follow it with WaitOperation).
type LoadBalancerAccepted struct {
	LoadBalancerID string    `json:"load_balancer_id"`
	Operation      Operation `json:"operation"`
}

func lbPath(vpcID, lbID string) string {
	p := vpcPath(vpcID) + "/load-balancers"
	if lbID != "" {
		p += "/" + PathEscape(lbID)
	}
	return p
}

// ListLoadBalancers returns a VPC's (id) balancers. Listener stickiness is
// left out of lists; GetLoadBalancer has it.
func (c *Client) ListLoadBalancers(ctx context.Context, vpcID string) ([]LoadBalancer, error) {
	return listAll[LoadBalancer](ctx, c, lbPath(vpcID, ""), ListOptions{})
}

// GetLoadBalancer reads one balancer (id) of a VPC (id).
func (c *Client) GetLoadBalancer(ctx context.Context, vpcID, lbID string) (*LoadBalancer, error) {
	var lb LoadBalancer
	return &lb, c.get(ctx, lbPath(vpcID, lbID), nil, &lb)
}

// FindLoadBalancer reads a VPC's (id) balancer by name, address or id.
func (c *Client) FindLoadBalancer(ctx context.Context, vpcID, ref string) (*LoadBalancer, error) {
	all, err := c.ListLoadBalancers(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	for _, lb := range all {
		if lb.Address != "" && lb.Address == ref {
			return c.GetLoadBalancer(ctx, vpcID, lb.ID)
		}
	}
	found, err := findRef(all, ref, "load balancer", func(lb LoadBalancer) (string, string) { return lb.ID, lb.Name })
	if err != nil {
		return nil, err
	}
	return c.GetLoadBalancer(ctx, vpcID, found.ID)
}

// CreateLoadBalancer reserves a new public address on the VPC (id) and
// builds the balancer on it. It needs an idempotency key.
func (c *Client) CreateLoadBalancer(ctx context.Context, vpcID string, body CreateLoadBalancer, idemKey string) (*LoadBalancerAccepted, error) {
	var out LoadBalancerAccepted
	return &out, c.Do(ctx, http.MethodPost, lbPath(vpcID, ""), body, &out, &RequestOptions{IdempotencyKey: idemKey})
}

// DeleteLoadBalancer deletes a balancer. Its address is kept unless
// releaseAddress.
func (c *Client) DeleteLoadBalancer(ctx context.Context, vpcID, lbID string, releaseAddress bool) (*Operation, error) {
	path := lbPath(vpcID, lbID)
	if releaseAddress {
		path += "?" + url.Values{"release_address": {"true"}}.Encode()
	}
	return c.lbChange(ctx, http.MethodDelete, path, nil, "")
}

// AddLoadBalancerMachines puts machines (ids, on the balancer's subnet)
// behind every listener. It needs an idempotency key.
func (c *Client) AddLoadBalancerMachines(ctx context.Context, vpcID, lbID string, machineIDs []string, idemKey string) (*Operation, error) {
	body := map[string]any{"machine_ids": machineIDs}
	return c.lbChange(ctx, http.MethodPost, lbPath(vpcID, lbID)+"/machines", body, idemKey)
}

// RemoveLoadBalancerMachine takes a machine (id) out of the balancer; the
// machine itself is not changed.
func (c *Client) RemoveLoadBalancerMachine(ctx context.Context, vpcID, lbID, machineID string) (*Operation, error) {
	return c.lbChange(ctx, http.MethodDelete, lbPath(vpcID, lbID)+"/machines/"+PathEscape(machineID), nil, "")
}

// AddListener adds a listener; the balancer's machines join it. It needs an
// idempotency key.
func (c *Client) AddListener(ctx context.Context, vpcID, lbID string, spec ListenerSpec, idemKey string) (*Operation, error) {
	return c.lbChange(ctx, http.MethodPost, lbPath(vpcID, lbID)+"/listeners", spec, idemKey)
}

// UpdateListener changes a listener's (id) algorithm or stickiness.
func (c *Client) UpdateListener(ctx context.Context, vpcID, lbID, listenerID string, changes ListenerChanges) (*Operation, error) {
	return c.lbChange(ctx, http.MethodPatch, lbPath(vpcID, lbID)+"/listeners/"+PathEscape(listenerID), changes, "")
}

// DeleteListener deletes a listener (id). The last one can't go: delete the
// balancer instead.
func (c *Client) DeleteListener(ctx context.Context, vpcID, lbID, listenerID string) (*Operation, error) {
	return c.lbChange(ctx, http.MethodDelete, lbPath(vpcID, lbID)+"/listeners/"+PathEscape(listenerID), nil, "")
}

// lbChange sends one balancer change; every change answers with the
// operation applying it.
func (c *Client) lbChange(ctx context.Context, method, path string, body any, idemKey string) (*Operation, error) {
	var op Operation
	var opts *RequestOptions
	if idemKey != "" {
		opts = &RequestOptions{IdempotencyKey: idemKey}
	}
	return &op, c.Do(ctx, method, path, body, &op, opts)
}
