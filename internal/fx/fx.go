// Package fx finds exchange rates, for entries paid in a currency other than
// their ledger's.
//
// Rates come from Frankfurter (the European Central Bank's reference rates,
// published each working day, and kept for every past day), so an entry is
// converted at its own date's rate. For a currency the ECB doesn't publish,
// such as TWD, today's rate comes from ExchangeRate-API's open endpoint
// instead. Neither needs a key; the requests carry only currency codes and a
// date. Rates are cached: a past day's for good, today's for an hour.
package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Rate is how much one unit of From is worth in To, as of Date.
type Rate struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Rate   float64 `json:"rate"`
	Date   string  `json:"date"`   // the day the rate was published (YYYY-MM-DD)
	Source string  `json:"source"` // who published it
}

// ecb are the currencies the European Central Bank publishes rates for.
var ecb = map[string]bool{}

func init() {
	for _, c := range strings.Fields("AUD BRL CAD CHF CNY CZK DKK EUR GBP HKD HUF IDR ILS INR ISK JPY KRW MXN MYR NOK NZD PHP PLN RON SEK SGD THB TRY USD ZAR") {
		ecb[c] = true
	}
}

var codeRE = regexp.MustCompile(`^[A-Z]{3}$`)

var (
	// ErrUnknown is a currency no source has a rate for.
	ErrUnknown = errors.New("no exchange rate is published for this currency")
	// ErrCode is a currency that isn't written as a code.
	ErrCode = errors.New("a currency is a three-letter code, such as USD")
)

// Rates finds and caches rates. The zero value isn't usable; use New.
type Rates struct {
	// The sources' base URLs, replaceable in tests.
	FrankfurterURL string
	OpenURL        string

	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	rate  Rate
	until time.Time // zero: for good
}

func New() *Rates {
	return &Rates{
		FrankfurterURL: "https://api.frankfurter.dev/v1",
		OpenURL:        "https://open.er-api.com/v6",
		client:         &http.Client{Timeout: 10 * time.Second},
		now:            time.Now,
		cache:          map[string]cached{},
	}
}

// Get returns the rate from one currency to another on a day (today's when
// the day is today or later, or the latest before it on a weekend).
func (r *Rates) Get(ctx context.Context, from, to string, day time.Time) (Rate, error) {
	from, to = strings.ToUpper(strings.TrimSpace(from)), strings.ToUpper(strings.TrimSpace(to))
	if !codeRE.MatchString(from) || !codeRE.MatchString(to) {
		return Rate{}, ErrCode
	}
	today := r.now().UTC().Format("2006-01-02")
	date := day.Format("2006-01-02")
	if from == to {
		return Rate{From: from, To: to, Rate: 1, Date: min(date, today), Source: "same currency"}, nil
	}
	latest := date >= today
	key := from + to + date
	if latest {
		key = from + to + "latest"
	}
	r.mu.Lock()
	c, ok := r.cache[key]
	r.mu.Unlock()
	if ok && (c.until.IsZero() || r.now().Before(c.until)) {
		return c.rate, nil
	}

	var rate Rate
	var err error
	switch {
	case ecb[from] && ecb[to]:
		rate, err = r.frankfurter(ctx, from, to, date, latest)
	default:
		// Only today's rate exists for these; a past day gets it too, as the closest there is.
		rate, err = r.open(ctx, from, to)
		latest = true
	}
	if err != nil {
		return Rate{}, err
	}
	until := time.Time{}
	if latest {
		until = r.now().Add(time.Hour)
	}
	r.mu.Lock()
	if len(r.cache) > 5000 {
		r.cache = map[string]cached{}
	}
	r.cache[key] = cached{rate: rate, until: until}
	r.mu.Unlock()
	return rate, nil
}

func (r *Rates) frankfurter(ctx context.Context, from, to, date string, latest bool) (Rate, error) {
	path := date
	if latest {
		path = "latest"
	}
	var body struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("%s/%s?base=%s&symbols=%s", r.FrankfurterURL, path, from, to), &body); err != nil {
		return Rate{}, err
	}
	v, ok := body.Rates[to]
	if !ok || v <= 0 {
		return Rate{}, ErrUnknown
	}
	return Rate{From: from, To: to, Rate: v, Date: body.Date, Source: "European Central Bank, via Frankfurter"}, nil
}

func (r *Rates) open(ctx context.Context, from, to string) (Rate, error) {
	var body struct {
		Result     string             `json:"result"`
		ErrorType  string             `json:"error-type"`
		LastUpdate int64              `json:"time_last_update_unix"`
		Rates      map[string]float64 `json:"rates"`
	}
	if err := r.fetch(ctx, fmt.Sprintf("%s/latest/%s", r.OpenURL, from), &body); err != nil {
		return Rate{}, err
	}
	if body.Result != "success" {
		return Rate{}, ErrUnknown
	}
	v, ok := body.Rates[to]
	if !ok || v <= 0 {
		return Rate{}, ErrUnknown
	}
	return Rate{From: from, To: to, Rate: v, Date: time.Unix(body.LastUpdate, 0).UTC().Format("2006-01-02"), Source: "ExchangeRate-API"}, nil
}

func (r *Rates) fetch(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Paylist")
	res, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the exchange rate service: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusUnprocessableEntity {
		return ErrUnknown
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the exchange rate service answered %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(into)
}
