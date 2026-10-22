package wec

import (
	"context"
	"net/http"
)

// AddonQuote is what a public address and backups cost on one plan, each
// bought as a plan of its own for the plan's term.
type AddonQuote struct {
	PlanID     string      `json:"plan_id"`
	TermMonths int         `json:"term_months"`
	PlanEndsAt string      `json:"plan_ends_at"`
	PlanRenews bool        `json:"plan_renews"`
	Backup     *AddonPrice `json:"backup"`
	IPAddress  *AddonPrice `json:"ip_address"`
}

// AddonPrice is one add-on's price. Total is what the wallet is charged (the
// quoted_total to send); Back comes straight back for the days past the
// plan's end; Net is the difference.
type AddonPrice struct {
	Total   float64 `json:"total"`
	Monthly float64 `json:"monthly"`
	Back    float64 `json:"back"`
	Net     float64 `json:"net"`
}

// Price returns the add-on of kind ("backup" or "ip_address"); nil when the
// plan's city doesn't sell it.
func (q *AddonQuote) Price(kind string) *AddonPrice {
	if kind == "backup" {
		return q.Backup
	}
	return q.IPAddress
}

func addonPath(planID, kind string) string {
	return "/plans/" + PathEscape(planID) + "/addons/" + kind
}

// QuoteAddons prices the add-ons on a plan (by its portal plan id).
func (c *Client) QuoteAddons(ctx context.Context, planID string) (*AddonQuote, error) {
	var q AddonQuote
	return &q, c.get(ctx, "/plans/"+PathEscape(planID)+"/addons/quote", nil, &q)
}

// AddAddress gives a plan's machine a public address on networkID: a new one,
// or publicIPID when the account reserved one. It spends from the wallet, so
// it needs an idempotency key; a moved price is refused with plan_price_changed.
func (c *Client) AddAddress(ctx context.Context, planID, networkID, publicIPID string, quotedTotal float64, idemKey string) (map[string]any, error) {
	body := map[string]any{"network_id": networkID, "quoted_total": quotedTotal}
	if publicIPID != "" {
		body["public_ip_id"] = publicIPID
	}
	return c.addonWrite(ctx, addonPath(planID, "ip_address"), body, idemKey)
}

// AddBackups turns a plan machine's backups on. Like AddAddress, it spends.
func (c *Client) AddBackups(ctx context.Context, planID string, quotedTotal float64, idemKey string) (map[string]any, error) {
	return c.addonWrite(ctx, addonPath(planID, "backup"), map[string]any{"quoted_total": quotedTotal}, idemKey)
}

// RemoveAddon takes an add-on plan ("backup" or "ip_address") off a plan: the
// address is released, or the backups deleted, and the whole days left are
// refunded. The route takes no idempotency key, so it is sent once.
func (c *Client) RemoveAddon(ctx context.Context, planID, kind string) (map[string]any, error) {
	return c.addonWrite(ctx, addonPath(planID, kind)+"/remove", nil, "")
}

func (c *Client) addonWrite(ctx context.Context, path string, body any, idemKey string) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, http.MethodPost, path, body, &out, &RequestOptions{IdempotencyKey: idemKey})
	return out, err
}

// ResolveAddressID turns a public address (the IP or its id) into its id.
func (c *Client) ResolveAddressID(ctx context.Context, address string) (string, error) {
	addresses, err := c.ListAddresses(ctx, ListOptions{})
	if err != nil {
		return "", err
	}
	return resolve(addresses, address, "address", func(a Address) (string, string) { return a.ID, a.IPAddress })
}
