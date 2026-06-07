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

func TestParseUserPositions(t *testing.T) {
	t.Parallel()

	// Mirrors a live getUserPositions response: a EUR holding, a USD holding,
	// a free bonus share (open_bal=0 → dropped), and EUR/USD cash. currval is
	// the currency→RUB rate (EUR 85, USD 73), used to total in EUR.
	raw := `{
		"pos":[
			{"i":"XEON.EU","name2":"Xtrackers","q":39,"market_value":5823.09,"mkt_price":149.319,"close_price":149.28,"open_bal":5812.88,"curr":"EUR","base_currency":"EUR","currval":85},
			{"i":"VUAA.EU","name2":"Vanguard","q":9,"market_value":1303.2,"mkt_price":144.8,"close_price":137.62,"open_bal":1249.62,"curr":"USD","base_currency":"USD","currval":73},
			{"i":"BB.US","name2":"BlackBerry","q":1,"market_value":9.1,"mkt_price":9.14,"close_price":9.41,"open_bal":0,"curr":"USD","currval":73}
		],
		"acc":[
			{"curr":"EUR","s":1.18,"currval":85},
			{"curr":"USD","s":89.63,"currval":73}
		]
	}`

	p, err := parseUserPositions(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parseUserPositions: %v", err)
	}
	if p.TotalCurrency != "€" {
		t.Errorf("currency = %q, want €", p.TotalCurrency)
	}
	// Bonus share dropped → 2 positions, sorted by EUR value (XEON first).
	if len(p.Positions) != 2 {
		t.Fatalf("positions = %d, want 2 (bonus share must be dropped)", len(p.Positions))
	}
	if p.Positions[0].Symbol != "XEON.EU" {
		t.Errorf("first symbol = %q, want XEON.EU", p.Positions[0].Symbol)
	}
	// VUAA shown in its own currency ($) with a positive day change.
	vuaa := p.Positions[1]
	if vuaa.Currency != "$" {
		t.Errorf("VUAA currency = %q, want $", vuaa.Currency)
	}
	if vuaa.Delta.Direction() != 1 {
		t.Errorf("VUAA should be up (144.8 > 137.62)")
	}
	// Total in EUR: XEON 5823.09 + VUAA 1303.2*73/85 + cash (1.18 + 89.63*73/85).
	wantTotal := 5823.09 + 1303.2*73.0/85.0 + 1.18 + 89.63*73.0/85.0
	if math.Abs(p.TotalValue-wantTotal) > 0.5 {
		t.Errorf("total = %v, want ~%v", p.TotalValue, wantTotal)
	}
	// Weights are fractions of the total and the largest holding leads.
	if p.Positions[0].Weight <= p.Positions[1].Weight {
		t.Errorf("XEON weight should exceed VUAA: %v vs %v", p.Positions[0].Weight, p.Positions[1].Weight)
	}
	if w := p.Positions[0].Weight; w <= 0 || w > 1 {
		t.Errorf("weight out of range: %v", w)
	}
}

func TestParseUserPositionsError(t *testing.T) {
	t.Parallel()

	if _, err := parseUserPositions(json.RawMessage(`{"error":"User is not found","code":7}`)); err == nil {
		t.Error("expected error for an error envelope")
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

	tests := []struct {
		name     string
		symbol   string
		raw      string
		wantOK   bool
		wantLast float64
		wantCurr string
		wantName string
		wantAbs  float64
		wantPct  float64
	}{
		{
			// Legacy shape: no ClosePrice, falls back to the API chg/pcp fields.
			name: "legacy chg/pcp fallback", symbol: "XEON.EU",
			raw:      `{"c":"XEON.EU","short_name":"Xtrackers","ltp":160.58,"chg":0.06,"pcp":0.04,"x_curr":"EUR"}`,
			wantOK:   true,
			wantLast: 160.58, wantCurr: "€", wantName: "Xtrackers", wantAbs: 0.06, wantPct: 0.04,
		},
		{
			// Real getSecurityInfo shape: last in ltp, prev close in ClosePrice,
			// currency in base_currency, and no pcp — the delta is computed.
			name: "ltp vs ClosePrice", symbol: "VUAA.EU",
			raw:      `{"c":"VUAA.EU","name":"Vanguard S&P 500","ltp":144.6,"ClosePrice":137.62,"chg":6.98,"base_currency":"USD"}`,
			wantOK:   true,
			wantLast: 144.6, wantCurr: "$", wantName: "Vanguard S&P 500", wantAbs: 6.98, wantPct: 5.0719,
		},
		{
			name: "no price is not ok", symbol: "EMPTY",
			raw: `{"c":"EMPTY"}`, wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, ok := parseQuote(tt.symbol, json.RawMessage(tt.raw))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if inst.Last != tt.wantLast || inst.Currency != tt.wantCurr || inst.Name != tt.wantName {
				t.Errorf("inst = %+v", inst)
			}
			if math.Abs(inst.Delta.Abs-tt.wantAbs) > 0.001 {
				t.Errorf("delta abs = %v, want %v", inst.Delta.Abs, tt.wantAbs)
			}
			if math.Abs(inst.Delta.Pct-tt.wantPct) > 0.001 {
				t.Errorf("delta pct = %v, want %v", inst.Delta.Pct, tt.wantPct)
			}
		})
	}
}
