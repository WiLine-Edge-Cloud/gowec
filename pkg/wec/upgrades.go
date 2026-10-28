package wec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// PlanLedger is the part of a machine plan's ledger record that upgrades and
// add-ons are priced from. Plan types only what its table shows; this reads
// the rest from Raw.
type PlanLedger struct {
	PlanID string `json:"plan_id"`
	VMID   string `json:"vm_id"`
	// Entitlements are the add-ons the plan holds: a line inside the plan's
	// own enrollment has no EnrollmentID; an add-on plan of its own has one.
	Entitlements []struct {
		Type         string  `json:"resource_type"`
		Status       string  `json:"status"`
		EnrollmentID *string `json:"enrollment_id"`
	} `json:"entitlements"`
	Addons []LedgerAddon `json:"addons"`
}

// LedgerAddon is an add-on plan of the plan's own, with what removing it now
// puts back (Refund).
type LedgerAddon struct {
	Type   string   `json:"resource_type"`
	Status string   `json:"status"`
	Price  *float64 `json:"price"`
	Refund *float64 `json:"refund"`
}

// LedgerOf reads a plan's ledger record; nil for add-on and data volume plans.
func LedgerOf(p *Plan) (*PlanLedger, error) {
	raw, ok := p.Raw["plan"]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var l PlanLedger
	return &l, json.Unmarshal(data, &l)
}

// UpgradeQuote is what moving a plan to a longer term costs, as the API
// prices it (GET /plans/{enrollment_id}/upgrade/quote).
type UpgradeQuote struct {
	TermMonths    int     `json:"term_months"`
	OldTermMonths int     `json:"old_term_months"`
	NewTotal      float64 `json:"new_total"` // the new term, with the add-on lines inside the plan
	Credit        float64 `json:"credit"`    // what is left of the current term
	NetTotal      float64 `json:"net_total"`
	RemainingDays int     `json:"remaining_days"`
	TotalDays     int     `json:"total_days"`
	// AddonMoves are the add-on plans of its own, bought again for the new
	// term (Total) less their unused days (Credit).
	AddonMoves []struct {
		Type   string  `json:"resource_type"`
		Total  float64 `json:"total"`
		Credit float64 `json:"credit"`
	} `json:"addon_moves"`
	Due float64 `json:"due"` // what the wallet is charged: the quoted_total to send
}

// QuoteUpgrade prices moving the plan of enrollmentID to a term of months.
func (c *Client) QuoteUpgrade(ctx context.Context, enrollmentID string, months int) (*UpgradeQuote, error) {
	var out UpgradeQuote
	q := url.Values{"term_months": {strconv.Itoa(months)}}
	return &out, c.get(ctx, "/plans/"+PathEscape(enrollmentID)+"/upgrade/quote", q, &out)
}

// UpgradeAccepted is the answer to POST /plans/{enrollment_id}/upgrade.
type UpgradeAccepted struct {
	UpgradeID string `json:"upgrade_id"`
	Status    string `json:"status"`
}

// Upgrade is a plan upgrade's progress.
type Upgrade struct {
	UpgradeID       string   `json:"upgrade_id"`
	Status          string   `json:"status"`
	PlanID          string   `json:"plan_id"`
	TermMonths      int      `json:"term_months"`
	OldTermMonths   int      `json:"old_term_months"`
	NewTotal        *float64 `json:"new_total"`
	Credit          *float64 `json:"credit"`
	NetTotal        *float64 `json:"net_total"`
	RemainingDays   *int     `json:"remaining_days"`
	TotalDays       *int     `json:"total_days"`
	NewEnrollmentID *string  `json:"new_enrollment_id"`
	NewEndsAt       *string  `json:"new_ends_at"`
	ErrorCode       *string  `json:"error_code"`
	ErrorDetail     *string  `json:"error_detail"`
	CreatedAt       *string  `json:"created_at"`
	FinishedAt      *string  `json:"finished_at"`
}

// Done reports whether the upgrade has finished, whichever way.
func (u *Upgrade) Done() bool {
	return u.Status == "done" || u.Status == "failed" || u.Status == "cancelled"
}

// UpgradePlan moves a running machine's plan (by its enrollment id, Plan.ID)
// to a longer term. It spends from the wallet, so it needs an idempotency
// key; a price that moved since quotedTotal is refused with
// plan_upgrade_price_changed.
func (c *Client) UpgradePlan(ctx context.Context, enrollmentID string, months int, quotedTotal float64, idemKey string) (*UpgradeAccepted, error) {
	var out UpgradeAccepted
	body := map[string]any{"term_months": months, "quoted_total": quotedTotal}
	err := c.Do(ctx, http.MethodPost, "/plans/"+PathEscape(enrollmentID)+"/upgrade", body, &out,
		&RequestOptions{IdempotencyKey: idemKey})
	return &out, err
}

// GetUpgrade reads a plan upgrade's progress.
func (c *Client) GetUpgrade(ctx context.Context, upgradeID string) (*Upgrade, error) {
	var u Upgrade
	return &u, c.get(ctx, "/upgrades/"+PathEscape(upgradeID), nil, &u)
}

// WaitUpgrade follows a plan upgrade until it is done, failed or cancelled.
func (c *Client) WaitUpgrade(ctx context.Context, upgradeID string) (*Upgrade, error) {
	var u *Upgrade
	err := poll(ctx, c, func() (bool, error) {
		var err error
		if u, err = c.GetUpgrade(ctx, upgradeID); err != nil {
			return false, err
		}
		return u.Done(), nil
	})
	return u, err
}
