package wec

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// Wallet is the prepaid balance. The billing engine owns this shape, so only
// the fields the CLI shows are typed; Raw keeps the rest.
type Wallet struct {
	CurrentBalance     float64        `json:"current_balance"`
	TotalEarned        float64        `json:"total_earned"`
	TotalUsed          float64        `json:"total_used"`
	IsActive           bool           `json:"is_active"`
	AutoTopupEnabled   bool           `json:"auto_topup_enabled"`
	AutoTopupThreshold *float64       `json:"auto_topup_threshold"`
	AutoTopupAmount    *float64       `json:"auto_topup_amount"`
	Raw                map[string]any `json:"-"`
}

// UnmarshalJSON keeps the whole object in Raw alongside the typed fields.
func (w *Wallet) UnmarshalJSON(data []byte) error {
	type plain Wallet
	if err := json.Unmarshal(data, (*plain)(w)); err != nil {
		return err
	}
	return json.Unmarshal(data, &w.Raw)
}

// MarshalJSON writes the object as the API sent it.
func (w Wallet) MarshalJSON() ([]byte, error) { return json.Marshal(w.Raw) }

// WalletTransaction is one movement of the wallet.
type WalletTransaction struct {
	ID              string  `json:"id"`
	TransactionType string  `json:"transaction_type"`
	Amount          float64 `json:"amount"`
	BalanceBefore   float64 `json:"balance_before"`
	BalanceAfter    float64 `json:"balance_after"`
	ReferenceType   string  `json:"reference_type,omitempty"`
	ReferenceID     string  `json:"reference_id,omitempty"`
	Description     string  `json:"description,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

// GetWallet reads the wallet's balance.
func (c *Client) GetWallet(ctx context.Context) (*Wallet, error) {
	var w Wallet
	return &w, c.get(ctx, "/wallet", nil, &w)
}

// ListWalletTransactions returns the latest movements (limit 1-100, 0 = the API's default).
func (c *Client) ListWalletTransactions(ctx context.Context, limit int) ([]WalletTransaction, error) {
	var q url.Values
	if limit > 0 {
		q = url.Values{"limit": {strconv.Itoa(limit)}}
	}
	var out struct {
		Transactions []WalletTransaction `json:"transactions"`
	}
	return out.Transactions, c.get(ctx, "/wallet/transactions", q, &out)
}
