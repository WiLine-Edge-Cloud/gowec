package wec

import (
	"context"
	"time"
)

// WhoAmI is the key making the call, its account, and who it acts as.
type WhoAmI struct {
	KeyID       string            `json:"key_id"`
	KeyName     string            `json:"key_name"`
	Last4       string            `json:"last4"`
	Env         string            `json:"env"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Permissions map[string]string `json:"permissions"`
	CanSpend    bool              `json:"can_spend"`
	CanDelete   bool              `json:"can_delete"`
	AccountID   string            `json:"account_id"`
	AccountName string            `json:"account_name,omitempty"`
	ActsAs      string            `json:"acts_as,omitempty"`
}

// WhoAmI checks the client's key and describes it.
func (c *Client) WhoAmI(ctx context.Context) (*WhoAmI, error) {
	var w WhoAmI
	return &w, c.get(ctx, "/whoami", nil, &w)
}
