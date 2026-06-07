// Package favqs fetches English quotes from favqs.com. With an API token it
// reads a page of quotes (rotated by the caller); without a token it falls back
// to the public quote-of-the-day endpoint.
package favqs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

const defaultBaseURL = "https://favqs.com/api"

// Client fetches quotes from favqs.com.
type Client struct {
	token   string
	baseURL string
	httpc   *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpc = h } }

// WithBaseURL overrides the API base URL (used in tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// New returns a favqs client. An empty token restricts the client to the
// quote-of-the-day endpoint.
func New(token string, timeout time.Duration, opts ...Option) *Client {
	c := &Client{
		token:   token,
		baseURL: defaultBaseURL,
		httpc:   &http.Client{Timeout: timeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Fetch returns a batch of quotes. With a token it reads a (randomised) page of
// the public list; otherwise it returns the single quote of the day.
func (c *Client) Fetch(ctx context.Context) ([]model.Quote, error) {
	if c.token == "" {
		q, err := c.qotd(ctx)
		if err != nil {
			return nil, err
		}
		return []model.Quote{q}, nil
	}
	return c.list(ctx)
}

func (c *Client) list(ctx context.Context) ([]model.Quote, error) {
	page := rand.IntN(30) + 1 // vary the page across refreshes
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/quotes?page="+strconv.Itoa(page), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", `Token token="`+c.token+`"`)

	body, err := c.do(req, "favqs quotes")
	if err != nil {
		return nil, err
	}
	var r struct {
		Quotes []struct {
			Body   string `json:"body"`
			Author string `json:"author"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode quotes: %w", err)
	}
	out := make([]model.Quote, 0, len(r.Quotes))
	for _, q := range r.Quotes {
		if q.Body == "" {
			continue
		}
		out = append(out, model.Quote{Text: q.Body, Author: q.Author})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("favqs returned no quotes")
	}
	return out, nil
}

func (c *Client) qotd(ctx context.Context) (model.Quote, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/qotd", nil)
	if err != nil {
		return model.Quote{}, fmt.Errorf("new request: %w", err)
	}
	body, err := c.do(req, "favqs qotd")
	if err != nil {
		return model.Quote{}, err
	}
	var r struct {
		Quote struct {
			Body   string `json:"body"`
			Author string `json:"author"`
		} `json:"quote"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return model.Quote{}, fmt.Errorf("decode qotd: %w", err)
	}
	return model.Quote{Text: r.Quote.Body, Author: r.Quote.Author}, nil
}

func (c *Client) do(req *http.Request, what string) ([]byte, error) {
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", what, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: http %d", what, resp.StatusCode)
	}
	return body, nil
}
