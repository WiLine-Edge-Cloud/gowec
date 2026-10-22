package wec

import (
	"context"
	"encoding/json"
	"net/url"
)

// Price is a machine size with its price. The billing engine owns this
// shape, so only the fields the CLI shows are typed; Raw keeps the rest.
type Price struct {
	City          string   `json:"city"`
	PricePerHour  *float64 `json:"price_per_hour"`
	PricePerMonth *float64 `json:"price_per_month"`
	Offering      struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		CPUCount int    `json:"cpunumber"`
		MemoryMB int    `json:"memory"`
	} `json:"cloudstack_offering"`
	Raw map[string]any `json:"-"`
}

func (p *Price) UnmarshalJSON(data []byte) error {
	type plain Price
	return keepRaw(data, (*plain)(p), &p.Raw)
}

func (p Price) MarshalJSON() ([]byte, error) { return json.Marshal(p.Raw) }

// ListPrices returns the machine sizes with their prices.
func (c *Client) ListPrices(ctx context.Context) ([]Price, error) {
	var out struct {
		Offerings []Price `json:"offerings"`
	}
	err := c.get(ctx, "/prices", nil, &out)
	return out.Offerings, err
}

// Bundle is a machine bundle for sale: a size, a root disk and a term at one
// price. The billing engine owns this shape; Raw keeps what isn't typed.
type Bundle struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	TermMonths   int            `json:"term_months"`
	PackagePrice *float64       `json:"package_price"`
	City         string         `json:"city"`
	CPUCount     int            `json:"cpunumber"`
	MemoryMB     int            `json:"memory_mb"`
	RootDiskGB   float64        `json:"root_disk_gb"`
	Addons       []BundleAddon  `json:"addons"`
	Raw          map[string]any `json:"-"`
}

// BundleAddon is an add-on a bundle can be bought with.
type BundleAddon struct {
	Type  string   `json:"resource_type"`
	Price *float64 `json:"addon_price"`
}

func (b *Bundle) UnmarshalJSON(data []byte) error {
	type plain Bundle
	return keepRaw(data, (*plain)(b), &b.Raw)
}

func (b Bundle) MarshalJSON() ([]byte, error) { return json.Marshal(b.Raw) }

// ListBundles returns the bundles for sale, optionally in one city (a zone's
// name, lowercased).
func (c *Client) ListBundles(ctx context.Context, city string) ([]Bundle, error) {
	opts := ListOptions{}
	if city != "" {
		opts.Extra = url.Values{"city": {city}}
	}
	return listAll[Bundle](ctx, c, "/bundles", opts)
}

// keepRaw decodes data into typed and keeps the whole object in raw.
func keepRaw(data []byte, typed any, raw *map[string]any) error {
	if err := json.Unmarshal(data, typed); err != nil {
		return err
	}
	return json.Unmarshal(data, raw)
}
