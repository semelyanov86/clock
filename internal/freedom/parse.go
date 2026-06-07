package freedom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

type opqPos struct {
	Ticker      string    `json:"i"`
	Name        string    `json:"name"`
	Name2       string    `json:"name2"`
	Qty         flexFloat `json:"q"`
	S           flexFloat `json:"s"`            // value converted to home currency
	MarketValue flexFloat `json:"market_value"` // value in the position currency
	MktPrice    flexFloat `json:"mkt_price"`    // current price
	ClosePrice  flexFloat `json:"close_price"`  // previous close (for daily change)
	ProfitClose flexFloat `json:"profit_close"` // total P&L since opening
	Curr        string    `json:"curr"`
	CurrVal     flexFloat `json:"currval"` // position currency → home rate
}

type opqAcc struct {
	Curr    string    `json:"curr"`
	S       flexFloat `json:"s"`
	CurrVal flexFloat `json:"currval"`
}

type opqResponse struct {
	OPQ struct {
		Ps struct {
			Pos []opqPos `json:"pos"`
			Acc []opqAcc `json:"acc"`
		} `json:"ps"`
		HomeCurrency string `json:"homeCurrency"`
	} `json:"OPQ"`
}

func parsePortfolio(raw json.RawMessage) (model.Portfolio, error) {
	var r opqResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return model.Portfolio{}, fmt.Errorf("decode getOPQ: %w", err)
	}
	ps := r.OPQ.Ps

	var total, totalDelta float64
	positions := make([]model.Position, 0, len(ps.Pos))
	for _, p := range ps.Pos {
		qty := float64(p.Qty)
		mv := float64(p.MarketValue)
		if mv == 0 {
			mv = float64(p.MktPrice) * qty
		}
		// Daily change from previous close.
		var dayAbs, dayPct float64
		if c := float64(p.ClosePrice); c != 0 {
			dayAbs = (float64(p.MktPrice) - c) * qty
			dayPct = (float64(p.MktPrice) - c) / c * 100
		} else {
			dayAbs = float64(p.ProfitClose)
		}
		positions = append(positions, model.Position{
			Symbol:   p.Ticker,
			Name:     firstNonEmpty(p.Name2, p.Name, p.Ticker),
			Qty:      qty,
			Value:    mv,
			Currency: currencySymbol(p.Curr),
			Delta:    model.Delta{Abs: dayAbs, Pct: dayPct},
		})

		// Totals in home currency.
		homeValue := float64(p.S)
		if homeValue == 0 {
			homeValue = mv * nonZero(float64(p.CurrVal), 1)
		}
		total += homeValue
		totalDelta += dayAbs * nonZero(float64(p.CurrVal), 1)
	}
	for _, a := range ps.Acc {
		total += float64(a.S) * nonZero(float64(a.CurrVal), 1)
	}

	sort.SliceStable(positions, func(i, j int) bool {
		return positions[i].Value > positions[j].Value
	})

	totalPct := 0.0
	if base := total - totalDelta; base != 0 {
		totalPct = totalDelta / base * 100
	}

	return model.Portfolio{
		TotalValue:    total,
		TotalCurrency: currencySymbol(r.OPQ.HomeCurrency),
		TotalDelta:    model.Delta{Abs: totalDelta, Pct: totalPct},
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

type secInfo struct {
	C     string    `json:"c"`
	Name  string    `json:"name"`
	Name2 string    `json:"name2"`
	Short string    `json:"short_name"`
	Ltp   flexFloat `json:"ltp"`
	L     flexFloat `json:"l"`
	Last  flexFloat `json:"last"`
	Chg   flexFloat `json:"chg"`
	Pcp   flexFloat `json:"pcp"`
	Curr  string    `json:"curr"`
	XCurr string    `json:"x_curr"`
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
	return model.Instrument{
		Symbol:   symbol,
		Name:     firstNonEmpty(s.Short, s.Name, s.Name2, symbol),
		Last:     last,
		Currency: currencySymbol(firstNonEmpty(s.XCurr, s.Curr)),
		Delta:    model.Delta{Abs: float64(s.Chg), Pct: float64(s.Pcp)},
	}, true
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
