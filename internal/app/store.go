package app

import (
	"sync"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

// store holds the latest value from each data source. It is safe for concurrent
// use: fetchers write, the frame loop reads.
type store struct {
	mu        sync.RWMutex
	weather   model.Weather
	portfolio model.Portfolio
	etfs      []model.Instrument
	brent     model.Instrument
	fx        []model.Instrument
	news      []model.NewsItem
	quotes    []model.Quote
	claude    model.ProviderUsage
	codex     model.ProviderUsage
}

func newStore() *store { return &store{} }

func (s *store) setWeather(w model.Weather) {
	s.mu.Lock()
	s.weather = w
	s.mu.Unlock()
}

func (s *store) setPortfolio(p model.Portfolio) {
	s.mu.Lock()
	s.portfolio = p
	s.mu.Unlock()
}

func (s *store) setMarkets(etfs []model.Instrument, brent model.Instrument, fx []model.Instrument) {
	s.mu.Lock()
	s.etfs = etfs
	s.brent = brent
	s.fx = fx
	s.mu.Unlock()
}

func (s *store) setNews(n []model.NewsItem) {
	s.mu.Lock()
	s.news = n
	s.mu.Unlock()
}

func (s *store) setQuotes(q []model.Quote) {
	s.mu.Lock()
	s.quotes = q
	s.mu.Unlock()
}

func (s *store) setClaude(u model.ProviderUsage) {
	s.mu.Lock()
	u.Stale = false
	s.claude = u
	s.mu.Unlock()
}

func (s *store) markClaudeStale() {
	s.mu.Lock()
	if s.claude.Available() {
		s.claude.Stale = true
	}
	s.mu.Unlock()
}

func (s *store) setCodex(u model.ProviderUsage) {
	s.mu.Lock()
	u.Stale = false
	s.codex = u
	s.mu.Unlock()
}

func (s *store) markCodexStale() {
	s.mu.Lock()
	if s.codex.Available() {
		s.codex.Stale = true
	}
	s.mu.Unlock()
}

// snapshot returns an immutable copy of the current data for one frame.
func (s *store) snapshot(now time.Time) model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return model.Snapshot{
		Generated: now,
		Weather:   s.weather,
		Portfolio: s.portfolio,
		ETFs:      append([]model.Instrument(nil), s.etfs...),
		Brent:     s.brent,
		FX:        append([]model.Instrument(nil), s.fx...),
		News:      append([]model.NewsItem(nil), s.news...),
		Quotes:    append([]model.Quote(nil), s.quotes...),
		Claude:    s.claude,
		Codex:     s.codex,
	}
}
