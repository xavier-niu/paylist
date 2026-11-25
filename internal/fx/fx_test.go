package fx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGet(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.URL.Path == "/f/latest":
			w.Write([]byte(`{"date":"2026-11-24","rates":{"CNY":6.6645}}`))
		case r.URL.Path == "/f/2026-11-20":
			w.Write([]byte(`{"date":"2026-11-20","rates":{"CNY":6.6777}}`))
		case r.URL.Path == "/o/latest/TWD":
			w.Write([]byte(`{"result":"success","time_last_update_unix":1795564951,"rates":{"CNY":0.2033}}`))
		case r.URL.Path == "/o/latest/XYZ":
			w.Write([]byte(`{"result":"error","error-type":"unsupported-code"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := New()
	r.FrankfurterURL, r.OpenURL = srv.URL+"/f", srv.URL+"/o"
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	ctx := context.Background()

	// Today: the latest rate, by base and symbol.
	got, err := r.Get(ctx, "usd", "CNY", now)
	if err != nil || got.Rate != 6.6645 || got.Date != "2026-11-24" || !strings.Contains(got.Source, "European Central Bank") {
		t.Fatalf("today: %+v %v", got, err)
	}
	if calls[0] != "/f/latest?base=USD&symbols=CNY" {
		t.Fatalf("asked %q", calls[0])
	}
	// A past day: that day's rate.
	got, err = r.Get(ctx, "USD", "CNY", time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	if err != nil || got.Rate != 6.6777 || got.Date != "2026-11-20" {
		t.Fatalf("past: %+v %v", got, err)
	}
	// Cached: asking again doesn't call out.
	n := len(calls)
	r.Get(ctx, "USD", "CNY", now)
	r.Get(ctx, "USD", "CNY", time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	if len(calls) != n {
		t.Fatalf("not cached: %v", calls)
	}
	// Today's rate is fetched again after an hour.
	now = now.Add(61 * time.Minute)
	r.Get(ctx, "USD", "CNY", now)
	if len(calls) != n+1 {
		t.Fatalf("today's rate not refreshed: %v", calls)
	}
	// A currency the ECB doesn't publish: the open source, today's rate.
	got, err = r.Get(ctx, "TWD", "CNY", now)
	if err != nil || got.Rate != 0.2033 || got.Source != "ExchangeRate-API" {
		t.Fatalf("TWD: %+v %v", got, err)
	}
	// The same currency needs no call.
	n = len(calls)
	if got, _ := r.Get(ctx, "CNY", "CNY", now); got.Rate != 1 || len(calls) != n {
		t.Fatalf("same: %+v", got)
	}
	// Unknown and malformed.
	if _, err := r.Get(ctx, "XYZ", "CNY", now); !errors.Is(err, ErrUnknown) {
		t.Fatalf("XYZ: %v", err)
	}
	if _, err := r.Get(ctx, "dollars", "CNY", now); !errors.Is(err, ErrCode) {
		t.Fatal("a malformed code passed")
	}
}
