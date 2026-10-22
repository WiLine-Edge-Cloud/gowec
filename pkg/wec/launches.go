package wec

import (
	"context"
	"net/http"
)

// LaunchMachine is the body of POST /launches: a prepaid machine and its plan.
// Zone, template, size and network take a name or an id.
type LaunchMachine struct {
	Name          string  `json:"name"`
	Zone          string  `json:"zone"`
	Template      string  `json:"template"`
	Size          string  `json:"size"`
	DiskGB        int     `json:"disk_gb"`
	Network       string  `json:"network"`
	TermMonths    int     `json:"term_months"`
	QuotedTotal   float64 `json:"quoted_total"`
	Backups       bool    `json:"backups"`
	PublicAddress bool    `json:"public_address"`
	SSHKey        string  `json:"ssh_key,omitempty"`
	AutoRenew     bool    `json:"auto_renew"`
}

// LaunchAccepted is the answer to POST /launches.
type LaunchAccepted struct {
	LaunchID string `json:"launch_id"`
	Status   string `json:"status"`
}

// Launch is a prepaid launch's progress.
type Launch struct {
	LaunchID     string   `json:"launch_id"`
	Status       string   `json:"status"`
	Step         string   `json:"step,omitempty"`
	VMID         string   `json:"vm_id,omitempty"`
	VMName       string   `json:"vm_name,omitempty"`
	IPAddress    string   `json:"ip_address,omitempty"`
	EnrollmentID string   `json:"enrollment_id,omitempty"`
	PlanID       string   `json:"plan_id,omitempty"`
	QuotedTotal  *float64 `json:"quoted_total,omitempty"`
	ErrorCode    string   `json:"error_code,omitempty"`
	ErrorDetail  string   `json:"error_detail,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	FinishedAt   string   `json:"finished_at,omitempty"`
}

// Done reports whether the launch has finished, either way.
func (l *Launch) Done() bool { return l.Status == "succeeded" || l.Status == "failed" }

// CreateLaunch starts a prepaid launch. It spends from the wallet, so it
// needs an idempotency key; a price that moved since the quote is refused
// with launch_price_changed.
func (c *Client) CreateLaunch(ctx context.Context, body LaunchMachine, idemKey string) (*LaunchAccepted, error) {
	var out LaunchAccepted
	err := c.Do(ctx, http.MethodPost, "/launches", body, &out, &RequestOptions{IdempotencyKey: idemKey})
	return &out, err
}

// GetLaunch reads a launch's progress.
func (c *Client) GetLaunch(ctx context.Context, launchID string) (*Launch, error) {
	var l Launch
	return &l, c.get(ctx, "/launches/"+PathEscape(launchID), nil, &l)
}
