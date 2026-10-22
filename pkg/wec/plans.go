package wec

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// PlanQuote is what a machine of one size costs, per term.
type PlanQuote struct {
	OfferingID  string      `json:"cloudstack_offering_id"`
	ServiceName string      `json:"service_name"`
	City        string      `json:"city"`
	DiskGB      float64     `json:"disk_gb"`
	Currency    string      `json:"currency"`
	Terms       []QuoteTerm `json:"terms"`
}

// QuoteTerm is one term's price.
type QuoteTerm struct {
	TermMonths       int                `json:"term_months"`
	Available        bool               `json:"available"`
	Reason           string             `json:"reason,omitempty"`
	PackagePrice     float64            `json:"package_price"`
	AddonPrices      map[string]float64 `json:"addon_prices"`
	MonthlyEffective float64            `json:"monthly_effective"`
	Lines            []QuoteLine        `json:"lines"`
}

// QuoteLine is one priced line of a term.
type QuoteLine struct {
	ResourceType string  `json:"resource_type"`
	Label        string  `json:"label"`
	Amount       float64 `json:"amount"`
}

// Term returns the quote's term of the given length.
func (q *PlanQuote) Term(months int) (*QuoteTerm, error) {
	for i := range q.Terms {
		if q.Terms[i].TermMonths == months {
			return &q.Terms[i], nil
		}
	}
	return nil, fmt.Errorf("no %d-month term for this size", months)
}

// Total is what a launch on this term costs: the package plus the add-ons
// chosen. It is the quoted_total POST /launches checks.
func (t *QuoteTerm) Total(backups, publicAddress bool) float64 {
	total := t.PackagePrice
	if backups {
		total += t.AddonPrices["backup"]
	}
	if publicAddress {
		total += t.AddonPrices["ip_address"]
	}
	return math.Round(total*100) / 100
}

// QuotePlan prices a machine of size (name or id) with a root disk of diskGB.
func (c *Client) QuotePlan(ctx context.Context, size string, diskGB int) (*PlanQuote, error) {
	var q PlanQuote
	query := url.Values{"size": {size}, "disk_gb": {strconv.Itoa(diskGB)}}
	return &q, c.get(ctx, "/plans/quote", query, &q)
}

// Plan is one of the account's plans (a deal enrollment). The billing engine
// owns this shape, so only the fields the CLI shows are typed; Raw keeps the rest.
type Plan struct {
	ID            string   `json:"id"`
	Status        string   `json:"status"`
	DealName      string   `json:"deal_name"`
	DisplayName   string   `json:"display_name"`
	TermMonths    int      `json:"term_months"`
	AutoRenew     bool     `json:"auto_renew"`
	EndsAt        string   `json:"ends_at"`
	VMID          string   `json:"vm_id"`
	Charged       *float64 `json:"charged_amount"`
	PaymentStatus string   `json:"payment_status"`
	// Ledger is the portal's record of a machine's plan; nil on add-on and
	// data volume plans, which carry Addon or VolumePlan instead.
	Ledger *struct {
		PlanID string      `json:"plan_id"`
		Status string      `json:"status"`
		VMID   string      `json:"vm_id"`
		DiskGB float64     `json:"disk_gb"`
		Addons []PlanAddon `json:"addons"`
	} `json:"plan"`
	Addon *struct {
		PlanID string `json:"plan_id"`
	} `json:"addon"`
	VolumePlan map[string]any `json:"volume_plan"`
	Raw        map[string]any `json:"-"`
}

// PlanAddon is an add-on plan (an address or backups) bought for a machine's plan.
type PlanAddon struct {
	Type       string   `json:"resource_type"`
	Status     string   `json:"status"`
	Price      *float64 `json:"price"`
	TermMonths int      `json:"term_months"`
}

// Ended reports whether the plan no longer runs: ended, cancelled, or unpaid.
func (p *Plan) Ended() bool {
	switch strings.ToLower(p.Status) {
	case "expired", "cancelled", "canceled", "inactive", "fulfilled", "ended":
		return true
	}
	switch strings.ToLower(p.PaymentStatus) {
	case "past_due", "failed":
		return true
	}
	return p.Ledger != nil && p.Ledger.Status == "ended"
}

// Monthly is what the plan costs a month: what was paid for the term spread
// over it, plus each add-on plan in force at its own price over its own term.
func (p *Plan) Monthly() (float64, bool) {
	if p.Charged == nil || p.TermMonths == 0 {
		return 0, false
	}
	total := *p.Charged / float64(p.TermMonths)
	for _, a := range p.addonsInForce() {
		if a.Price != nil && a.TermMonths > 0 {
			total += *a.Price / float64(a.TermMonths)
		}
	}
	return total, true
}

// Paid is what the plan and its add-ons cost for the whole term.
func (p *Plan) Paid() float64 {
	var total float64
	if p.Charged != nil {
		total = *p.Charged
	}
	for _, a := range p.addonsInForce() {
		if a.Price != nil {
			total += *a.Price
		}
	}
	return total
}

func (p *Plan) addonsInForce() []PlanAddon {
	if p.Ledger == nil {
		return nil
	}
	var out []PlanAddon
	for _, a := range p.Ledger.Addons {
		if a.Status == "available" || a.Status == "claimed" {
			out = append(out, a)
		}
	}
	return out
}

// PlansByMachine maps each machine id to the live plan it runs on.
func PlansByMachine(plans []Plan) map[string]*Plan {
	out := map[string]*Plan{}
	for i := range plans {
		p := &plans[i]
		if p.Ledger != nil && p.Ledger.VMID != "" && p.Ledger.Status == "active" && !p.Ended() {
			out[p.Ledger.VMID] = p
		}
	}
	return out
}

// UnmarshalJSON keeps the whole object in Raw alongside the typed fields.
func (p *Plan) UnmarshalJSON(data []byte) error {
	type plain Plan
	if err := json.Unmarshal(data, (*plain)(p)); err != nil {
		return err
	}
	return json.Unmarshal(data, &p.Raw)
}

// MarshalJSON writes the object as the API sent it.
func (p Plan) MarshalJSON() ([]byte, error) { return json.Marshal(p.Raw) }

// ListPlans returns the account's plans.
func (c *Client) ListPlans(ctx context.Context) ([]Plan, error) {
	var out struct {
		Enrollments []Plan `json:"enrollments"`
	}
	return out.Enrollments, c.get(ctx, "/plans", nil, &out)
}

// DeletePlanNow deletes a plan's machine now, and the plan with it; the
// unused days go back to the wallet. Data volumes are kept, detached.
func (c *Client) DeletePlanNow(ctx context.Context, planID, idemKey string) (map[string]any, error) {
	var out map[string]any
	err := c.Do(ctx, http.MethodPost, "/plans/"+PathEscape(planID)+"/delete-now", nil, &out,
		&RequestOptions{IdempotencyKey: idemKey})
	return out, err
}
