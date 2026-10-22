// Package wec is a client for the WEC public API (/api/public/v1).
//
// It is shared by the gowec CLI and, later, the Terraform provider: the HTTP
// plumbing, retries, idempotency keys, paging and job waiting live here so
// both tools behave the same way.
package wec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIPrefix is appended to the host the client is given.
const APIPrefix = "/api/public/v1"

const maxAttempts = 3

// Client calls the public API with one key.
type Client struct {
	BaseURL   string // the host, e.g. https://prepaid-cloudapi.wiline.com
	Token     string // wec_<env>_<id>_<secret>
	UserAgent string
	HTTP      *http.Client

	// sleep is swapped out by tests.
	sleep func(context.Context, time.Duration) error
}

// New returns a client for baseURL using token.
func New(baseURL, token, userAgent string) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     token,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
	}
}

// RequestOptions are the extras a single call can carry.
type RequestOptions struct {
	Query          url.Values
	IdempotencyKey string
}

// Do sends one request and decodes a JSON answer into out (when out is not nil).
//
// GETs are retried on network errors, 5xx and 429. Writes are retried only
// when they carry an idempotency key (the same key every time, so the server
// replays rather than acting twice), or on 429, which the server refused
// before doing anything.
func (c *Client) Do(ctx context.Context, method, path string, body, out any, opts *RequestOptions) error {
	if opts == nil {
		opts = &RequestOptions{}
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	endpoint := c.BaseURL + APIPrefix + path
	if len(opts.Query) > 0 {
		endpoint += "?" + opts.Query.Encode()
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.send(ctx, method, endpoint, payload, opts.IdempotencyKey)
		retryable := method == http.MethodGet || opts.IdempotencyKey != ""
		if err != nil {
			if ctx.Err() != nil || !retryable || attempt == maxAttempts {
				return err
			}
			if err := c.wait(ctx, backoff(attempt)); err != nil {
				return err
			}
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode < 300 {
			if out == nil || len(data) == 0 {
				return nil
			}
			return json.Unmarshal(data, out)
		}

		apiErr := parseError(resp, data)
		again := resp.StatusCode == http.StatusTooManyRequests || (retryable && transient(resp.StatusCode, apiErr))
		if !again || attempt == maxAttempts {
			return apiErr
		}
		delay := backoff(attempt)
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 {
			delay = time.Duration(s) * time.Second
		}
		if err := c.wait(ctx, delay); err != nil {
			return err
		}
	}
}

// transient reports whether a 5xx may pass on a retry: the service was
// briefly unavailable, or something in front of the API answered. A 500 or
// 502 in the API's own error shape is its answer, and a retry would only
// repeat it.
func transient(status int, apiErr *APIError) bool {
	switch {
	case status == http.StatusServiceUnavailable, status == http.StatusGatewayTimeout:
		return true
	case status >= 500:
		return apiErr.Extra == nil // not the API's own error shape
	}
	return false
}

func (c *Client) send(ctx context.Context, method, endpoint string, payload []byte, idemKey string) (*http.Response, error) {
	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't reach %s: %w", c.BaseURL, err)
	}
	return resp, nil
}

func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	return sleepCtx(ctx, d)
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out, &RequestOptions{Query: query})
}

// PathEscape escapes one path segment (names may carry spaces).
func PathEscape(s string) string { return url.PathEscape(s) }
