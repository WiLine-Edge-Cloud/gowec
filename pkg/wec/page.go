package wec

import (
	"context"
	"net/url"
	"strconv"
)

// Page is one page of a public list.
type Page[T any] struct {
	Items   []T `json:"items"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// ListOptions filter a list. Name is an exact, any-case match.
type ListOptions struct {
	Name   string
	Status string
	Extra  url.Values // e.g. zone_id
}

func (o ListOptions) query() url.Values {
	q := url.Values{}
	for k, v := range o.Extra {
		q[k] = v
	}
	if o.Name != "" {
		q.Set("name", o.Name)
	}
	if o.Status != "" {
		q.Set("status", o.Status)
	}
	return q
}

// listAll walks every page of path with the largest page size.
func listAll[T any](ctx context.Context, c *Client, path string, opts ListOptions) ([]T, error) {
	q := opts.query()
	q.Set("per_page", "200")
	var all []T
	for page := 1; ; page++ {
		q.Set("page", strconv.Itoa(page))
		var p Page[T]
		if err := c.get(ctx, path, q, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Items...)
		if len(p.Items) == 0 || len(all) >= p.Total {
			return all, nil
		}
	}
}
