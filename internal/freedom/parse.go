package freedom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

// flexFloat decodes a JSON number that the API may return as a number, a
// string, or an empty value. Unparseable input becomes 0 rather than failing
// the whole document (Tradernet mixes number and string encodings).
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" || string(b) == `""` {
		*f = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			*f = flexFloat(v)
		}
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err == nil {
		*f = flexFloat(v)
	}
	return nil
}

type authResponse struct {
	Success bool   `json:"success"`
	Logged  bool   `json:"logged"`
	SID     string `json:"SID"`
	Error   string `json:"error"`
	ErrMsg  string `json:"errMsg"`
	Code    int    `json:"code"`
}

// parseAuth validates the authByLogin response and returns the SID.
func parseAuth(raw json.RawMessage) (string, error) {
	var r authResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode auth response: %w", err)
	}
	if msg := firstNonEmpty(r.Error, r.ErrMsg); msg != "" {
		return "", errors.New(msg)
	}
	if r.Code != 0 {
		return "", fmt.Errorf("code %d", r.Code)
	}
	if r.SID == "" || (!r.Success && !r.Logged) {
		return "", errors.New("login rejected (no SID returned)")
	}
	return r.SID, nil
}

// looksUnauthorized detects an expired/invalid session so the caller can
// re-authenticate and retry. It decodes the boolean flags rather than matching
// raw text, so it is insensitive to server JSON spacing.
func looksUnauthorized(raw json.RawMessage) bool {
	var r struct {
		Logged  *bool `json:"logged"`
		Success *bool `json:"success"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	if r.Logged != nil && !*r.Logged {
		return true
	}
	if r.Success != nil && !*r.Success {
		return true
	}
	return false
}

// userPos is one holding from getUserPositions. Values are in the position's
// own currency (curr/base_currency); currval converts that currency to RUB.
type userPos struct {
	Ticker      string    `json:"i"`
	Name        string    `json:"name"`
	Name2       string    `json:"name2"`
	Qty         flexFloat `json:"q"`
	MarketValue flexFloat `json:"market_value"` // value in the position currency
	MktPrice    flexFloat `json:"mkt_price"`    // current price
	ClosePrice  flexFloat `json:"close_price"`  // previous close
	OpenBal     flexFloat `json:"open_bal"`     // cost basis; 0 ⇒ free bonus share
	ProfitClose flexFloat `json:"profit_close"` // total P&L since purchase (own currency)
	Curr        string    `json:"curr"`
	BaseCurr    string    `json:"base_currency"`
	CurrVal     flexFloat `json:"currval"` // position currency → RUB rate
}

// userAcc is one cash balance from getUserPositions.
type userAcc struct {
	Curr    string    `json:"curr"`
	S       flexFloat `json:"s"` // cash amount in curr
	CurrVal flexFloat `json:"currval"`
}

// moneyDetail is one per-currency block from money_detailed; rate is that
// currency's canonical account exchange rate to RUB.
type moneyDetail struct {
	Currency string    `json:"currency"`
	Rate     flexFloat `json:"rate"` // currency → RUB
}

// netAssets is the broker's authoritative total account equity (the figure the
// Freedom24 app shows), denominated in the account's tariff currency.
type netAssets struct {
	Currency  string    `json:"currency"`
	NetAssets flexFloat `json:"net_assets"`
}

// totals mirrors net_assets: total_trade_positions equals net_assets and serves
// as a fallback when the net_assets block is absent.
type totals struct {
	Currency            string    `json:"currency"`
	TotalTradePositions flexFloat `json:"total_trade_positions"`
}

type userPositionsResponse struct {
	Pos           []userPos              `json:"pos"`
	Acc           []userAcc              `json:"acc"`
	Totals        totals                 `json:"totals"`
	NetAssets     netAssets              `json:"net_assets"`
	MoneyDetailed map[string]moneyDetail `json:"money_detailed"`
	Error         string                 `json:"error"`
	ErrMsg        string                 `json:"errMsg"`
}

// parseUserPositions builds the portfolio from a getUserPositions response.
//
// Total value comes from the broker's own net_assets figure (the number the
// Freedom24 app shows), converted to EUR — hand-summing per-position values
// diverges from it because market_value and the per-row currval are inconsistent
// across holdings. The per-position and total "dynamics" is the total P&L since
// purchase (profit_close vs open_bal cost basis), not the daily change: the
// low-liquidity ETFs held here are marked at the previous close, so
// mkt_price==close_price and any day change would be a spurious 0. Per-position
// cards keep the instrument's own currency; free bonus shares (open_bal=0) are
// hidden. Weights are each holding's share of the invested positions.
func parseUserPositions(raw json.RawMessage) (model.Portfolio, error) {
	var r userPositionsResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return model.Portfolio{}, fmt.Errorf("decode getUserPositions: %w", err)
	}
	if msg := firstNonEmpty(r.Error, r.ErrMsg); msg != "" {
		return model.Portfolio{}, errors.New(msg)
	}

	// Currency→RUB rate table. money_detailed carries the account's canonical FX
	// rates; positions'/cash currval fill any gaps.
	rateRUB := map[string]float64{}
	for cur, md := range r.MoneyDetailed {
		if md.Rate != 0 {
			rateRUB[strings.ToUpper(cur)] = float64(md.Rate)
		}
	}
	addRate := func(cur string, v float64) {
		cur = strings.ToUpper(strings.TrimSpace(cur))
		if cur == "" || v == 0 {
			return
		}
		if _, ok := rateRUB[cur]; !ok {
			rateRUB[cur] = v
		}
	}
	for _, p := range r.Pos {
		addRate(firstNonEmpty(p.Curr, p.BaseCurr), float64(p.CurrVal))
	}
	for _, a := range r.Acc {
		addRate(a.Curr, float64(a.CurrVal))
	}

	eurRub := rateRUB["EUR"]
	if eurRub == 0 {
		eurRub = 1.0
	}
	// toEUR converts an amount in currency cur to EUR via the RUB cross-rate.
	toEUR := func(amount float64, cur string) float64 {
		cur = strings.ToUpper(strings.TrimSpace(cur))
		if cur == "" || cur == "EUR" {
			return amount
		}
		rate := rateRUB[cur]
		if rate == 0 {
			return amount // best effort: treat as already in the home currency
		}
		return amount * rate / eurRub
	}

	type valued struct {
		pos model.Position
		eur float64
	}
	rows := make([]valued, 0, len(r.Pos))
	var posSumEUR, pnlEUR, costEUR float64
	for _, p := range r.Pos {
		if float64(p.OpenBal) == 0 {
			continue // free bonus share — no cost basis, hide it
		}
		qty := float64(p.Qty)
		cur := firstNonEmpty(p.Curr, p.BaseCurr)
		valOwn := float64(p.MarketValue)
		if valOwn == 0 {
			valOwn = float64(p.MktPrice) * qty
		}
		pnl := float64(p.ProfitClose)
		cost := float64(p.OpenBal)
		var pnlPct float64
		if cost != 0 {
			pnlPct = pnl / cost * 100
		}
		eur := toEUR(valOwn, cur)
		rows = append(rows, valued{
			pos: model.Position{
				Symbol:   p.Ticker,
				Name:     firstNonEmpty(p.Name2, p.Name, p.Ticker),
				Qty:      qty,
				Value:    valOwn,
				Currency: currencySymbol(cur),
				Delta:    model.Delta{Abs: pnl, Pct: pnlPct},
			},
			eur: eur,
		})
		posSumEUR += eur
		pnlEUR += toEUR(pnl, cur)
		costEUR += toEUR(cost, cur)
	}

	// Authoritative total: the broker's net_assets (fallback: totals, then a
	// hand-sum of positions + cash), converted to EUR.
	total := 0.0
	netVal := nonZero(float64(r.NetAssets.NetAssets), float64(r.Totals.TotalTradePositions))
	switch {
	case netVal != 0:
		total = toEUR(netVal, firstNonEmpty(r.NetAssets.Currency, r.Totals.Currency))
	default:
		total = posSumEUR
		for _, a := range r.Acc {
			total += toEUR(float64(a.S), a.Curr)
		}
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].eur > rows[j].eur })
	positions := make([]model.Position, 0, len(rows))
	for _, rw := range rows {
		if posSumEUR > 0 {
			rw.pos.Weight = rw.eur / posSumEUR
		}
		positions = append(positions, rw.pos)
	}

	totalPct := 0.0
	if costEUR != 0 {
		totalPct = pnlEUR / costEUR * 100
	}

	return model.Portfolio{
		TotalValue:    total,
		TotalCurrency: "€",
		TotalDelta:    model.Delta{Abs: pnlEUR, Pct: totalPct},
		Positions:     positions,
	}, nil
}

type reviewItem struct {
	Title    string          `json:"title"`
	Name     string          `json:"name"`
	Header   string          `json:"header"`
	Descr    string          `json:"description"`
	Date     json.RawMessage `json:"date"`
	Datetime json.RawMessage `json:"datetime"`
	PubDate  json.RawMessage `json:"pub_date"`
	URL      string          `json:"url"`
}

func extractNews(raw json.RawMessage, n int) []model.NewsItem {
	arr := findArray(raw)
	out := make([]model.NewsItem, 0, n)
	for _, el := range arr {
		var it reviewItem
		if err := json.Unmarshal(el, &it); err != nil {
			continue
		}
		title := firstNonEmpty(it.Title, it.Header, it.Name, it.Descr)
		if title == "" {
			continue
		}
		out = append(out, model.NewsItem{
			Title:  strings.TrimSpace(title),
			Date:   parseFlexibleTime(it.Date, it.Datetime, it.PubDate),
			Source: "Freedom24",
		})
		if len(out) == n {
			break
		}
	}
	return out
}

// findArray returns the first JSON array found among common container keys,
// descending one level into nested objects if needed.
func findArray(raw json.RawMessage) []json.RawMessage {
	keys := []string{"reviews", "items", "result", "data", "mr", "list", "rows"}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	for _, k := range keys {
		v, ok := top[k]
		if !ok {
			continue
		}
		if arr := asArray(v); arr != nil {
			return arr
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(v, &nested) == nil {
			for _, nk := range keys {
				if arr := asArray(nested[nk]); arr != nil {
					return arr
				}
			}
		}
	}
	return nil
}

func asArray(v json.RawMessage) []json.RawMessage {
	if len(v) == 0 {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(v, &arr); err != nil {
		return nil
	}
	return arr
}

// secInfo maps the getSecurityInfo fields we need. Confirmed against live
// responses (XEON.EU, VUAA.EU, EUR/RUR, …): the last trade price is `ltp`, the
// trading currency is `base_currency`, and the day change is reported both as a
// percentage (`pcp`) and an absolute (`chg`), with `ClosePrice` as the third
// source. See parseQuote for how the three are ranked.
type secInfo struct {
	C            string    `json:"c"`
	Name         string    `json:"name"`
	Name2        string    `json:"name2"`
	Short        string    `json:"short_name"`
	Ltp          flexFloat `json:"ltp"`
	L            flexFloat `json:"l"`
	Last         flexFloat `json:"last"`
	Chg          flexFloat `json:"chg"`
	Pcp          flexFloat `json:"pcp"`
	ClosePrice   flexFloat `json:"ClosePrice"`
	Curr         string    `json:"curr"`
	XCurr        string    `json:"x_curr"`
	BaseCurrency string    `json:"base_currency"`
}

func parseQuote(symbol string, raw json.RawMessage) (model.Instrument, bool) {
	var s secInfo
	_ = json.Unmarshal(raw, &s)
	if s.Ltp == 0 && s.L == 0 && s.Last == 0 {
		// Some responses wrap the quote in a container.
		var wrap struct {
			Result secInfo `json:"result"`
			Info   secInfo `json:"info"`
		}
		if json.Unmarshal(raw, &wrap) == nil {
			if wrap.Result.Ltp != 0 || wrap.Result.L != 0 || wrap.Result.Last != 0 {
				s = wrap.Result
			} else {
				s = wrap.Info
			}
		}
	}
	last := firstNonZero(float64(s.Ltp), float64(s.L), float64(s.Last))
	if last == 0 {
		return model.Instrument{}, false
	}

	delta := quoteDelta(last, float64(s.Pcp), float64(s.Chg), float64(s.ClosePrice))

	return model.Instrument{
		Symbol:   symbol,
		Name:     firstNonEmpty(s.Short, s.Name, s.Name2, symbol),
		Last:     last,
		Currency: currencySymbol(firstNonEmpty(s.XCurr, s.Curr, s.BaseCurrency)),
		Delta:    delta,
	}, true
}

// quoteDelta picks the day change out of the three numbers getSecurityInfo
// reports, in order of how much they can be trusted (verified against live
// responses on 2026-08-16):
//
//   - `pcp`, the feed's own percentage, wins whenever it is reported.
//   - `ClosePrice` is used only as a fallback, because it lies in two ways: on FX
//     pairs it is years stale (EUR/RUR carried 80.1 against a live 97.70, which
//     rendered as a fake ▲+22 %), and on exchange instruments it is rolled to the
//     last trade once the session closes, which flattens the day to 0.00 %.
//   - `chg`, the absolute change, is only taken when it agrees with `pcp` — on FX
//     pairs it is derived from the same stale close (EUR/RUR reported +17.60).
//     Otherwise the absolute change is implied from the percentage.
func quoteDelta(last, pcp, chg, prevClose float64) model.Delta {
	if pcp == 0 {
		if prevClose != 0 {
			abs := last - prevClose
			return model.Delta{Abs: abs, Pct: abs / prevClose * 100}
		}
		return model.Delta{Abs: chg}
	}

	d := model.Delta{Pct: pcp}
	if factor := 1 + pcp/100; factor != 0 {
		d.Abs = last - last/factor
	}
	// Prefer the feed's own absolute change when the two agree (within a tenth of
	// the implied move), so the reported precision is kept.
	if chg != 0 && d.Abs != 0 && math.Abs(chg-d.Abs) <= 0.1*math.Abs(d.Abs) {
		d.Abs = chg
	}
	return d
}

func parseFlexibleTime(vals ...json.RawMessage) time.Time {
	for _, v := range vals {
		if len(v) == 0 || string(v) == "null" {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0)
		}
		s := strings.Trim(string(v), `"`)
		// A Unix timestamp may also arrive as a quoted string.
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0)
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func currencySymbol(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "EUR":
		return "€"
	case "USD":
		return "$"
	case "RUB", "RUR":
		return "₽"
	case "CNY", "CNH":
		return "¥"
	case "GBP":
		return "£"
	case "":
		return ""
	default:
		return code
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(vals ...float64) float64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

func nonZero(v, fallback float64) float64 {
	if v != 0 {
		return v
	}
	return fallback
}
