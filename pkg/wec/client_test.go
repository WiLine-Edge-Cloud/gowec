package wec

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(url string) *Client {
	c := New(url, "wec_test_k_s", "gowec/test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

// A write is retried only with an idempotency key, and always with the same
// one, so the server replays instead of spending twice.
func TestWritesRetryOnlyWithTheSameIdempotencyKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"launch_id":"l1","status":"pending"}`))
	}))
	defer srv.Close()

	out, err := testClient(srv.URL).CreateLaunch(context.Background(), LaunchMachine{Name: "web-1"}, "idem-1")
	if err != nil || out.LaunchID != "l1" {
		t.Fatalf("launch = %+v, %v", out, err)
	}
	if len(keys) != 2 || keys[0] != "idem-1" || keys[1] != "idem-1" {
		t.Fatalf("idempotency keys sent = %v", keys)
	}

	keys = nil
	_, err = testClient(srv.URL).StartMachine(context.Background(), "web-1")
	if err == nil || len(keys) != 1 {
		t.Fatalf("a write without a key was retried: %d calls, err %v", len(keys), err)
	}
}

func TestErrorEnvelopeIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "hdr-id")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":{"code":"launch_price_changed","message":"The price changed.",` +
			`"request_id":"req-1","quoted_total":24.4}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).CreateLaunch(context.Background(), LaunchMachine{}, "k")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err = %T %v", err, err)
	}
	if apiErr.Status != 409 || apiErr.Code != "launch_price_changed" || apiErr.RequestID != "req-1" {
		t.Fatalf("parsed = %+v", apiErr)
	}
	if total, _ := apiErr.Extra["quoted_total"].(float64); math.Abs(total-24.4) > 1e-9 {
		t.Fatalf("extra = %v", apiErr.Extra)
	}
}

func TestQuoteTotalAddsTheChosenAddons(t *testing.T) {
	term := QuoteTerm{PackagePrice: 21.9, AddonPrices: map[string]float64{"backup": 0.4, "ip_address": 3.5}}
	cases := []struct {
		backups, address bool
		want             float64
	}{
		{false, false, 21.9},
		{true, true, 25.8},
	}
	for _, c := range cases {
		if got := term.Total(c.backups, c.address); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Total(%v, %v) = %v, want %v", c.backups, c.address, got, c.want)
		}
	}
}
