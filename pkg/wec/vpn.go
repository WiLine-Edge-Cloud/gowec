package wec

import (
	"context"
	"net/http"
)

// VPNGateway is a VPC's end of its tunnels, on the VPC's source NAT address.
type VPNGateway struct {
	ID       string `json:"id"`
	PublicIP string `json:"public_ip,omitempty"`
	VPCCIDR  string `json:"vpc_cidr,omitempty"`
}

// VPNLimits are the platform's caps: connections per VPC, ranges per office.
type VPNLimits struct {
	Connections     int `json:"connections"`
	RangesPerOffice int `json:"ranges_per_office"`
}

// VPNOfficeSettings is an office (a customer gateway): their router and how
// the tunnel is negotiated. The pre-shared key is never returned.
type VPNOfficeSettings struct {
	ID               string   `json:"id"`
	Name             string   `json:"name,omitempty"`
	Gateway          string   `json:"gateway,omitempty"`
	CIDRList         []string `json:"cidr_list"`
	IKEPolicy        string   `json:"ike_policy,omitempty"`
	ESPPolicy        string   `json:"esp_policy,omitempty"`
	IKEVersion       string   `json:"ike_version,omitempty"`
	IKELifetime      int      `json:"ike_lifetime,omitempty"`
	ESPLifetime      int      `json:"esp_lifetime,omitempty"`
	DPD              bool     `json:"dpd"`
	ForceEncap       bool     `json:"force_encap"`
	SplitConnections bool     `json:"split_connections"`
}

// VPNOfficeConnection is one VPC an office is connected to.
type VPNOfficeConnection struct {
	ID      string `json:"id"`
	VPCID   string `json:"vpc_id,omitempty"`
	VPCName string `json:"vpc_name,omitempty"`
	State   string `json:"state,omitempty"`
	Passive bool   `json:"passive"`
	Created string `json:"created,omitempty"`
}

// VPNOffice is an office with the VPCs it connects to.
type VPNOffice struct {
	VPNOfficeSettings
	Connections []VPNOfficeConnection `json:"connections"`
}

// VPNConnection is a tunnel between a VPC and an office. State is Pending,
// Connecting, Connected, Disconnected or Error.
type VPNConnection struct {
	ID      string            `json:"id"`
	State   string            `json:"state,omitempty"`
	Passive bool              `json:"passive"`
	Created string            `json:"created,omitempty"`
	Office  VPNOfficeSettings `json:"office"`
	// PeerVPC is set when the office is another of the account's VPCs (a
	// link): remove it with UnlinkVPC, which takes both ends.
	PeerVPC *VPNPeerVPC `json:"peer_vpc,omitempty"`
}

// VPNPeerVPC is the other VPC of a link.
type VPNPeerVPC struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// VPNRouterSettings is everything their router needs for one connection,
// the pre-shared key included. Reading it needs network:manage and is audited.
type VPNRouterSettings struct {
	Office   VPNOfficeSettings `json:"office"`
	PublicIP string            `json:"public_ip,omitempty"`
	VPCCIDR  string            `json:"vpc_cidr,omitempty"`
	IPsecPSK string            `json:"ipsec_psk,omitempty"`
}

// Operation is a multi-step action the server carries out (linking or
// unlinking VPCs). Status is pending, running, succeeded or failed; an empty
// OperationID means it already finished inside the request.
type Operation struct {
	OperationID  string `json:"operation_id,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Status       string `json:"status"`
	Step         string `json:"step,omitempty"`
	ResourceName string `json:"resource_name,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorDetail  string `json:"error_detail,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

// Done reports whether the operation has ended.
func (o *Operation) Done() bool { return o.Status == "succeeded" || o.Status == "failed" }

// VPCVPN is a VPC's VPN: its gateway (nil until enabled), the limits and its
// connections.
type VPCVPN struct {
	Gateway     *VPNGateway     `json:"gateway"`
	Limits      VPNLimits       `json:"limits"`
	Connections []VPNConnection `json:"connections"`
}

// CreateVPNOffice is the body of POST /vpn/offices.
type CreateVPNOffice struct {
	Name             string   `json:"name,omitempty"`
	Gateway          string   `json:"gateway"`
	CIDRList         []string `json:"cidr_list"`
	IPsecPSK         string   `json:"ipsec_psk"`
	IKEPolicy        string   `json:"ike_policy"`
	ESPPolicy        string   `json:"esp_policy"`
	IKEVersion       string   `json:"ike_version,omitempty"`
	IKELifetime      int      `json:"ike_lifetime,omitempty"`
	ESPLifetime      int      `json:"esp_lifetime,omitempty"`
	DPD              bool     `json:"dpd"`
	ForceEncap       bool     `json:"force_encap"`
	SplitConnections bool     `json:"split_connections"`
}

// UpdateVPNOffice is the body of PATCH /vpn/offices/{id}: nil fields are
// kept, and so is the stored key unless IPsecPSK is set.
type UpdateVPNOffice struct {
	Name             *string  `json:"name,omitempty"`
	Gateway          *string  `json:"gateway,omitempty"`
	CIDRList         []string `json:"cidr_list,omitempty"`
	IPsecPSK         *string  `json:"ipsec_psk,omitempty"`
	IKEPolicy        *string  `json:"ike_policy,omitempty"`
	ESPPolicy        *string  `json:"esp_policy,omitempty"`
	IKEVersion       *string  `json:"ike_version,omitempty"`
	IKELifetime      *int     `json:"ike_lifetime,omitempty"`
	ESPLifetime      *int     `json:"esp_lifetime,omitempty"`
	DPD              *bool    `json:"dpd,omitempty"`
	ForceEncap       *bool    `json:"force_encap,omitempty"`
	SplitConnections *bool    `json:"split_connections,omitempty"`
}

func vpnPath(vpcID string) string { return vpcPath(vpcID) + "/vpn" }

func officePath(officeID string) string { return "/vpn/offices/" + PathEscape(officeID) }

// GetVPCVPN reads a VPC's (id) VPN.
func (c *Client) GetVPCVPN(ctx context.Context, vpcID string) (*VPCVPN, error) {
	var v VPCVPN
	return &v, c.get(ctx, vpnPath(vpcID), nil, &v)
}

// EnableVPN creates a VPC's (id) VPN gateway. It needs an idempotency key.
func (c *Client) EnableVPN(ctx context.Context, vpcID, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, vpnPath(vpcID)+"/gateway", nil, nil, idemKey)
}

// DisableVPN removes a VPC's (id) VPN gateway; its connections go first.
func (c *Client) DisableVPN(ctx context.Context, vpcID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, vpnPath(vpcID)+"/gateway", nil, nil, "")
}

// ListVPNOffices returns the account's offices and where each is connected.
func (c *Client) ListVPNOffices(ctx context.Context, opts ListOptions) ([]VPNOffice, error) {
	return listAll[VPNOffice](ctx, c, "/vpn/offices", opts)
}

// FindVPNOffice reads one office by name or id.
func (c *Client) FindVPNOffice(ctx context.Context, office string) (*VPNOffice, error) {
	all, err := c.ListVPNOffices(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	return findRef(all, office, "office", func(o VPNOffice) (string, string) { return o.ID, o.Name })
}

// CreateVPNOffice saves an office, connected nowhere yet. It needs an
// idempotency key; the accepted ResourceID is the office's id.
func (c *Client) CreateVPNOffice(ctx context.Context, body CreateVPNOffice, idemKey string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, "/vpn/offices", nil, body, idemKey)
}

// UpdateVPNOffice changes an office (id); its tunnels restart on every VPC.
func (c *Client) UpdateVPNOffice(ctx context.Context, officeID string, body UpdateVPNOffice) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPatch, officePath(officeID), nil, body, "")
}

// DeleteVPNOffice deletes an office (id) nothing connects to.
func (c *Client) DeleteVPNOffice(ctx context.Context, officeID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, officePath(officeID), nil, nil, "")
}

// ConnectVPN connects a VPC (id) to an office (id). It needs an idempotency
// key; the accepted ResourceID is the connection's id.
func (c *Client) ConnectVPN(ctx context.Context, vpcID, officeID string, passive bool, idemKey string) (*JobAccepted, error) {
	body := map[string]any{"office_id": officeID, "passive": passive}
	return c.machineAction(ctx, http.MethodPost, vpnPath(vpcID)+"/connections", nil, body, idemKey)
}

// FindVPNConnection finds one of a VPC's (id) connections by its id or its
// office's name.
func (c *Client) FindVPNConnection(ctx context.Context, vpcID, ref string) (*VPNConnection, error) {
	v, err := c.GetVPCVPN(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	return findRef(v.Connections, ref, "connection", func(x VPNConnection) (string, string) { return x.ID, x.Office.Name })
}

// ResetVPNConnection drops a tunnel and negotiates it again.
func (c *Client) ResetVPNConnection(ctx context.Context, vpcID, connectionID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodPost, vpnPath(vpcID)+"/connections/"+PathEscape(connectionID)+"/reset", nil, nil, "")
}

// DeleteVPNConnection deletes a tunnel; the office stays saved.
func (c *Client) DeleteVPNConnection(ctx context.Context, vpcID, connectionID string) (*JobAccepted, error) {
	return c.machineAction(ctx, http.MethodDelete, vpnPath(vpcID)+"/connections/"+PathEscape(connectionID), nil, nil, "")
}

// GetVPNRouterSettings reads a connection's router settings, key included.
func (c *Client) GetVPNRouterSettings(ctx context.Context, vpcID, connectionID string) (*VPNRouterSettings, error) {
	var s VPNRouterSettings
	return &s, c.get(ctx, vpnPath(vpcID)+"/connections/"+PathEscape(connectionID)+"/settings", nil, &s)
}

// LinkVPC links a VPC (id) to another of the account's (id): an office on
// each, one key nobody sees. It needs an idempotency key; follow the answer
// with WaitOperation.
func (c *Client) LinkVPC(ctx context.Context, vpcID, peerVPCID, idemKey string) (*Operation, error) {
	var op Operation
	body := map[string]any{"peer_vpc_id": peerVPCID}
	return &op, c.Do(ctx, http.MethodPost, vpnPath(vpcID)+"/links", body, &op, &RequestOptions{IdempotencyKey: idemKey})
}

// UnlinkVPC removes a link from either end (a connection id on vpcID):
// both connections, then both offices.
func (c *Client) UnlinkVPC(ctx context.Context, vpcID, connectionID string) (*Operation, error) {
	var op Operation
	return &op, c.Do(ctx, http.MethodDelete, vpnPath(vpcID)+"/links/"+PathEscape(connectionID), nil, &op, nil)
}

// GetOperation reads an operation by id.
func (c *Client) GetOperation(ctx context.Context, operationID string) (*Operation, error) {
	var op Operation
	return &op, c.get(ctx, "/operations/"+PathEscape(operationID), nil, &op)
}
