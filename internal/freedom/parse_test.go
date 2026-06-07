package freedom

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParseAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantSID string
		wantErr bool
	}{
		{"success", `{"success":true,"logged":true,"SID":"abc123"}`, "abc123", false},
		{"method error", `{"error":"User is not found","code":7}`, "", true},
		{"common error", `{"errMsg":"Bad json","code":2}`, "", true},
		{"no sid", `{"success":true,"logged":true}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sid, err := parseAuth(json.RawMessage(tt.in))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if sid != tt.wantSID {
				t.Errorf("sid = %q, want %q", sid, tt.wantSID)
			}
		})
	}
}

func TestParsePortfolio(t *testing.T) {
	t.Parallel()

	// Shape mirrors the documented getOPQ response.
	raw := `{"OPQ":{"homeCurrency":"USD","ps":{
		"acc":[{"curr":"USD","currval":1,"s":358}],
		"pos":[
			{"i":"AAPL.US","name":"Apple","q":10,"mkt_price":13.5,"close_price":11.5,"market_value":135,"s":135,"curr":"USD","currval":1},
			{"i":"SBER","name":"Sber","q":2,"mkt_price":9,"close_price":10,"market_value":18,"s":18,"curr":"USD","currval":1}
		]}}}`

	p, err := parsePortfolio(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parsePortfolio: %v", err)
	}
	if p.TotalCurrency != "$" {
		t.Errorf("currency = %q, want $", p.TotalCurrency)
	}
	// total = 135 + 18 + 358 = 511
	if math.Abs(p.TotalValue-511) > 0.001 {
		t.Errorf("total = %v, want 511", p.TotalValue)
	}
	if len(p.Positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(p.Positions))
	}
	// Sorted by value desc: AAPL (135) first.
	first := p.Positions[0]
	if first.Symbol != "AAPL.US" {
		t.Errorf("first symbol = %q, want AAPL.US", first.Symbol)
	}
	// day change = (13.5 - 11.5) * 10 = 20
	if math.Abs(first.Delta.Abs-20) > 0.001 {
		t.Errorf("AAPL day abs = %v, want 20", first.Delta.Abs)
	}
	if first.Delta.Direction() != 1 {
		t.Errorf("AAPL should be up")
	}
	// SBER day change negative
	if p.Positions[1].Delta.Direction() != -1 {
		t.Errorf("SBER should be down")
	}
}

func TestExtractNews(t *testing.T) {
	t.Parallel()

	raw := `{"reviews":[{"title":"Hello","date":1700000000},{"header":"World"},{"foo":"bar"}]}`
	news := extractNews(json.RawMessage(raw), 9)
	if len(news) != 2 {
		t.Fatalf("news = %d, want 2", len(news))
	}
	if news[0].Title != "Hello" || news[1].Title != "World" {
		t.Errorf("titles = %q, %q", news[0].Title, news[1].Title)
	}
	if news[0].Date.IsZero() {
		t.Error("first news date should be parsed from unix seconds")
	}
}

func TestParseQuote(t *testing.T) {
	t.Parallel()

	raw := `{"c":"XEON.EU","short_name":"Xtrackers","ltp":160.58,"chg":0.06,"pcp":0.04,"x_curr":"EUR"}`
	inst, ok := parseQuote("XEON.EU", json.RawMessage(raw))
	if !ok {
		t.Fatal("parseQuote returned not ok")
	}
	if inst.Last != 160.58 || inst.Currency != "€" || inst.Name != "Xtrackers" {
		t.Errorf("inst = %+v", inst)
	}
	if inst.Delta.Pct != 0.04 || inst.Delta.Abs != 0.06 {
		t.Errorf("delta = %+v", inst.Delta)
	}

	if _, ok := parseQuote("EMPTY", json.RawMessage(`{"c":"EMPTY"}`)); ok {
		t.Error("quote with no price should be not ok")
	}
}
