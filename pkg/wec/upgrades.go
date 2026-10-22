package wec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
)

// PlanLedger is the part of a machine plan's ledger record that upgrades and
// add-ons are priced from. Plan types only what its table shows; this reads
// the rest from Raw.
type PlanLedger struct {
	PlanID            string        `json:"plan_id"`
	VMID              string        `json:"vm_id"`
	ServiceOfferingID string        `json:"service_offering_id"`
	DiskGB            float64       `json:"disk_gb"`
	Upgrade           *UpgradeOffer `json:"upgrade"`
	UpgradeInFlight   bool          `json:"upgrade_in_flight"`
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
// puts back: Refund, after RepricedBy was taken off for an early exit.
type LedgerAddon struct {
	Type       string   `json:"resource_type"`
	Status     string   `json:"status"`
	TermMonths int      `json:"term_months"`
	Price      *float64 `json:"price"`
	Refund     *float64 `json:"refund"`
	RepricedBy *float64 `json:"repriced_by"`
}

// UpgradeOffer is what a running plan's unused days are worth toward a
// longer term (the listing's plan.upgrade).
type UpgradeOffer struct {
	Credit        float64 `json:"credit"`
	RemainingDays int     `json:"remaining_days"`
	TotalDays     int     `json:"total_days"`
	TermMonths    int     `json:"term_months"`
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

// UpgradePrice is what moving a plan to a longer term costs.
type UpgradePrice struct {
	TermMonths    int
	FromMonths    int
	NewTotal      float64 // the longer term, with the add-on lines inside the plan
	Credit        float64 // what is left of the current term
	AddonsTotal   float64 // the add-on plans of its own, bought again for the new term
	AddonsCredit  float64 // what is left of those add-on plans
	RemainingDays int
	Due           float64 // what the wallet is charged: the quoted_total to send
}

func cents(v float64) float64 { return math.Round(v*100) / 100 }

// PriceUpgrade prices plan p on a term of months from q, the quote for its
// size and disk, the way the API re-prices it: the longer term with every
// add-on line it holds, less the unused days, plus each add-on plan of its
// own bought again for the new term, less that one's unused days.
func PriceUpgrade(p *Plan, q *PlanQuote, months int) (*UpgradePrice, error) {
	ledger, err := LedgerOf(p)
	if err != nil {
		return nil, err
	}
	if ledger == nil || ledger.Upgrade == nil {
		return nil, errors.New("this plan can't be upgraded: it isn't a running machine's plan")
	}
	if ledger.UpgradeInFlight {
		return nil, errors.New("this plan is already being upgraded")
	}
	offer := ledger.Upgrade
	if months <= offer.TermMonths {
		return nil, fmt.Errorf("pick a term longer than the plan's %d months", offer.TermMonths)
	}
	term, err := q.Term(months)
	if err != nil {
		return nil, err
	}
	if !term.Available {
		return nil, fmt.Errorf("the %d-month term isn't sold for this machine: %s", months, term.Reason)
	}
	gross := term.PackagePrice
	for _, e := range ledger.Entitlements {
		if e.EnrollmentID != nil {
			continue
		}
		price, ok := term.AddonPrices[e.Type]
		if !ok {
			return nil, fmt.Errorf("the %d-month term isn't sold with this plan's %s", months, e.Type)
		}
		gross += price
	}
	gross = cents(gross)
	out := &UpgradePrice{TermMonths: months, FromMonths: offer.TermMonths, NewTotal: gross,
		Credit: math.Min(offer.Credit, gross), RemainingDays: offer.RemainingDays}
	for _, a := range ledger.Addons {
		if a.Status != "claimed" || a.TermMonths == months {
			continue
		}
		price, ok := term.AddonPrices[a.Type]
		if !ok {
			return nil, fmt.Errorf("the %d-month term isn't sold with this plan's %s", months, a.Type)
		}
		if a.Refund == nil {
			return nil, fmt.Errorf("this plan's %s can't be priced right now; try again shortly", a.Type)
		}
		// The move credits the add-on's unused days at the rate paid: the
		// listing's refund before any early-exit re-pricing.
		credit := *a.Refund
		if a.RepricedBy != nil {
			credit += *a.RepricedBy
		}
		out.AddonsTotal += price
		out.AddonsCredit += credit
	}
	out.AddonsTotal, out.AddonsCredit = cents(out.AddonsTotal), cents(out.AddonsCredit)
	out.Due = cents(cents(math.Max(gross-out.Credit, 0)) + out.AddonsTotal - out.AddonsCredit)
	return out, nil
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
