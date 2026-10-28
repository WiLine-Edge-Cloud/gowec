package wec

import (
	"context"
	"net/url"
)

// MonthQuote is what a one-month charge would take from the wallet now, as
// the API prices it without charging: zero when the machine's plan already
// pays for it (CoveredByPlan) or the account isn't prepaid.
type MonthQuote struct {
	Due   float64 `json:"due"`
	Lines []struct {
		Type   string  `json:"resource_type"`
		Label  string  `json:"label"`
		Amount float64 `json:"amount"`
	} `json:"lines"`
	CoveredByPlan bool `json:"covered_by_plan"`
	Prepaid       bool `json:"prepaid"`
}

// QuoteAddress prices allocating a public address in vpcID, optionally for a
// machine (id).
func (c *Client) QuoteAddress(ctx context.Context, vpcID, machineID string) (*MonthQuote, error) {
	q := url.Values{"vpc_id": {vpcID}}
	if machineID != "" {
		q.Set("machine_id", machineID)
	}
	var out MonthQuote
	return &out, c.get(ctx, "/addresses/quote", q, &out)
}

// QuoteBackups prices turning a machine's backups on (name or id).
func (c *Client) QuoteBackups(ctx context.Context, machine string) (*MonthQuote, error) {
	var out MonthQuote
	return &out, c.get(ctx, machinePath(machine)+"/backup-offering/quote", nil, &out)
}

// Offering is a kind of network or VPC that can be created.
type Offering struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayText string `json:"display_text,omitempty"`
	ForVPC      *bool  `json:"for_vpc,omitempty"`
}

func offeringKey(o Offering) (string, string) { return o.ID, o.Name }

// ListNetworkOfferings returns the network offerings, optionally of one zone (id).
func (c *Client) ListNetworkOfferings(ctx context.Context, zoneID string) ([]Offering, error) {
	return listAll[Offering](ctx, c, "/network-offerings", zoneQuery(zoneID))
}

// ListVPCOfferings returns the VPC offerings, optionally of one zone (id).
func (c *Client) ListVPCOfferings(ctx context.Context, zoneID string) ([]Offering, error) {
	return listAll[Offering](ctx, c, "/vpc-offerings", zoneQuery(zoneID))
}

// ResolveNetworkOffering turns a network offering's name or id into its id.
func (c *Client) ResolveNetworkOffering(ctx context.Context, ref string) (string, error) {
	all, err := c.ListNetworkOfferings(ctx, "")
	if err != nil {
		return "", err
	}
	return resolve(all, ref, "network_offering", offeringKey)
}

// ResolveVPCOffering turns a VPC offering's name or id into its id.
func (c *Client) ResolveVPCOffering(ctx context.Context, ref string) (string, error) {
	all, err := c.ListVPCOfferings(ctx, "")
	if err != nil {
		return "", err
	}
	return resolve(all, ref, "vpc_offering", offeringKey)
}
