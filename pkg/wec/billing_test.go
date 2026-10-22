package wec

import (
	"encoding/json"
	"math"
	"testing"
)

// The quote for a 2-CPU, 30 GB machine, as GET /plans/quote sent it.
const upgradeQuote = `{"currency":"USD","terms":[
 {"term_months":1,"available":true,"package_price":26.76,"addon_prices":{"backup":1.2,"ip_address":2}},
 {"term_months":12,"available":true,"package_price":321.12,"addon_prices":{"backup":14.4,"ip_address":24}}]}`

// An upgrade is the longer term with the plan's own add-on lines, less the
// unused days, plus each add-on plan bought again for the new term less its
// unused days at the rate paid (refund + repriced_by), as the API re-prices it.
func TestPriceUpgrade(t *testing.T) {
	var q PlanQuote
	if err := json.Unmarshal([]byte(upgradeQuote), &q); err != nil {
		t.Fatal(err)
	}
	var p Plan
	err := json.Unmarshal([]byte(`{"id":"enr-1","status":"active","term_months":1,"plan":{
		"plan_id":"plan-1","status":"active","vm_id":"vm-1",
		"upgrade":{"credit":24.17,"remaining_days":28,"total_days":31,"term_months":1},
		"entitlements":[{"resource_type":"ip_address","status":"claimed","enrollment_id":null},
		                {"resource_type":"backup","status":"claimed","enrollment_id":"enr-2"}],
		"addons":[{"resource_type":"backup","status":"claimed","term_months":1,"refund":1.0,"repriced_by":0.08}]}}`), &p)
	if err != nil {
		t.Fatal(err)
	}

	u, err := PriceUpgrade(&p, &q, 12)
	if err != nil {
		t.Fatal(err)
	}
	// 321.12 + 24 (the address line) - 24.17, then backups 14.40 - 1.08.
	if math.Abs(u.NewTotal-345.12) > 0.001 || math.Abs(u.Due-334.27) > 0.001 {
		t.Fatalf("upgrade = %+v, want new total 345.12 and due 334.27", u)
	}
	if _, err := PriceUpgrade(&p, &q, 1); err == nil {
		t.Fatal("an upgrade to the same term was priced")
	}
}
