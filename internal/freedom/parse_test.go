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

	// Mirrors a live getUserPositions response: EUR and USD holdings, a free
	// bonus share (open_bal=0 → dropped), and EUR/USD cash. money_detailed carries
	// the account FX rates to RUB (EUR 85, USD 73); net_assets is the broker's own
	// total account equity in USD, which is what the app shows. profit_close is the
	// position's total P&L since purchase (the dynamics shown on the card).
	raw := `{
		"totals":{"currency":"USD","total_trade_positions":8000},
		"net_assets":{"currency":"USD","net_assets":8000},
		"money_detailed":{"EUR":{"currency":"EUR","rate":85},"USD":{"currency":"USD","rate":73}},
		"pos":[
			{"i":"XEON.EU","name2":"Xtrackers","q":39,"market_value":5823.09,"mkt_price":149.28,"close_price":149.28,"open_bal":5812.88,"profit_close":10.21,"curr":"EUR","base_currency":"EUR","currval":85},
			{"i":"VUAA.EU","name2":"Vanguard","q":9,"market_value":1303.2,"mkt_price":144.8,"close_price":137.62,"open_bal":1249.62,"profit_close":53.58,"curr":"USD","base_currency":"USD","currval":73},
			{"i":"BB.US","name2":"BlackBerry","q":1,"market_value":9.1,"mkt_price":9.14,"close_price":9.41,"open_bal":0,"profit_close":5,"curr":"USD","currval":73}
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
	xeon := p.Positions[0]
	if xeon.Symbol != "XEON.EU" {
		t.Errorf("first symbol = %q, want XEON.EU", xeon.Symbol)
	}
	// XEON has no daily move (mkt_price==close_price) but a real P&L: the dynamics
	// must reflect profit_close/open_bal, not the (zero) day change.
	if wantPct := 10.21 / 5812.88 * 100; math.Abs(xeon.Delta.Pct-wantPct) > 0.001 {
		t.Errorf("XEON delta pct = %v, want %v (total P&L, not day change)", xeon.Delta.Pct, wantPct)
	}
	if math.Abs(xeon.Delta.Abs-10.21) > 0.001 {
		t.Errorf("XEON delta abs = %v, want profit_close 10.21", xeon.Delta.Abs)
	}
	// VUAA shown in its own currency ($) with a positive P&L.
	vuaa := p.Positions[1]
	if vuaa.Currency != "$" {
		t.Errorf("VUAA currency = %q, want $", vuaa.Currency)
	}
	if vuaa.Delta.Direction() != 1 {
		t.Errorf("VUAA should be up (profit_close 53.58 > 0)")
	}
	// Total comes from net_assets (8000 USD), converted to EUR — independent of
	// the per-position sum.
	if wantTotal := 8000.0 * 73.0 / 85.0; math.Abs(p.TotalValue-wantTotal) > 0.01 {
		t.Errorf("total = %v, want net_assets in EUR ~%v", p.TotalValue, wantTotal)
	}
	// Total dynamics = aggregate P&L in EUR (XEON 10.21 + VUAA 53.58*73/85).
	if wantPnL := 10.21 + 53.58*73.0/85.0; math.Abs(p.TotalDelta.Abs-wantPnL) > 0.01 {
		t.Errorf("total delta = %v, want aggregate P&L ~%v", p.TotalDelta.Abs, wantPnL)
	}
	// Weights are fractions of the invested positions and the largest leads.
	if xeon.Weight <= vuaa.Weight {
		t.Errorf("XEON weight should exceed VUAA: %v vs %v", xeon.Weight, vuaa.Weight)
	}
	if w := xeon.Weight; w <= 0 || w > 1 {
		t.Errorf("weight out of range: %v", w)
	}
}

// TestParseUserPositionsTotalFallback verifies the total falls back to a
// hand-sum of positions + cash when the broker omits net_assets/totals.
func TestParseUserPositionsTotalFallback(t *testing.T) {
	t.Parallel()

	raw := `{
		"money_detailed":{"EUR":{"currency":"EUR","rate":85},"USD":{"currency":"USD","rate":73}},
		"pos":[
			{"i":"XEON.EU","q":39,"market_value":5823.09,"open_bal":5812.88,"profit_close":10.21,"curr":"EUR"},
			{"i":"VUAA.EU","q":9,"market_value":1303.2,"open_bal":1249.62,"profit_close":53.58,"curr":"USD"}
		],
		"acc":[{"curr":"EUR","s":1.18},{"curr":"USD","s":89.63}]
	}`

	p, err := parseUserPositions(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parseUserPositions: %v", err)
	}
	wantTotal := 5823.09 + 1303.2*73.0/85.0 + 1.18 + 89.63*73.0/85.0
	if math.Abs(p.TotalValue-wantTotal) > 0.01 {
		t.Errorf("fallback total = %v, want hand-sum ~%v", p.TotalValue, wantTotal)
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
			// FX pair, live shape on 2026-08-16: ClosePrice is a 2023 leftover
			// (80.1 against a live 97.70) and chg is derived from it, so both are
			// ignored in favour of the feed's own pcp.
			name: "fx pair ignores stale ClosePrice", symbol: "EUR/RUR",
			raw: `{"c":"EUR/RUR","name":"Euro - Russian ruble","ltp":97.697425,"ClosePrice":80.1,` +
				`"chg":17.597425,"pcp":0.59,"base_currency":"EUR","x_curr":"RUR"}`,
			wantOK:   true,
			wantLast: 97.697425, wantCurr: "₽", wantName: "Euro - Russian ruble",
			wantAbs: 0.5729, wantPct: 0.59,
		},
		{
			// Session closed: the feed rolls ClosePrice to the last trade, which
			// would flatten a real −1.13% day to 0.00%. pcp still carries the move,
			// and chg agrees with it, so the reported absolute is kept.
			name: "closed session keeps the day move", symbol: "BRNT.EU",
			raw: `{"c":"BRNT.EU","name":"Brent","ltp":69.465,"ClosePrice":69.465,` +
				`"chg":-0.8,"pcp":-1.13,"base_currency":"EUR","x_curr":"EUR"}`,
			wantOK:   true,
			wantLast: 69.465, wantCurr: "€", wantName: "Brent",
			wantAbs: -0.8, wantPct: -1.13,
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
