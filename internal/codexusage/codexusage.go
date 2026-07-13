// Package codexusage reads the main Codex rate-limit bucket through the
// official Codex app-server protocol. Each fetch starts a short-lived local
// app-server process, performs the JSONL handshake, reads account limits, and
// exits without starting a model turn.
package codexusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

const (
	defaultBinary          = "codex"
	maxProtocolBytes       = 1 << 20
	maxStderrBytes         = 16 << 10
	fiveHourWindowDuration = 5 * time.Hour
	weeklyWindowDuration   = 7 * 24 * time.Hour
)

// Client reads Codex rate limits by invoking a local Codex CLI installation.
type Client struct {
	binary string
}

// New creates a Codex usage client. An empty binary uses "codex" from PATH.
func New(binary string) *Client {
	if strings.TrimSpace(binary) == "" {
		binary = defaultBinary
	}
	return &Client{binary: binary}
}

// Fetch starts a short-lived app-server process and returns its main Codex
// rate-limit bucket. CommandContext guarantees cancellation kills the process.
func (c *Client) Fetch(ctx context.Context) (model.ProviderUsage, error) {
	cmd := exec.CommandContext(ctx, c.binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return model.ProviderUsage{}, fmt.Errorf("open codex stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return model.ProviderUsage{}, errors.Join(
			fmt.Errorf("open codex stdout: %w", err),
			closeError("close codex stdin", stdin),
		)
	}

	var stderr cappedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return model.ProviderUsage{}, errors.Join(
			fmt.Errorf("start codex app-server: %w", err),
			closeError("close codex stdin", stdin),
			closeError("close codex stdout", stdout),
		)
	}

	usage, exchangeErr := exchange(io.LimitReader(stdout, maxProtocolBytes), stdin)
	closeErr := closeError("close codex stdin", stdin)
	_, drainErr := io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return model.ProviderUsage{}, fmt.Errorf("query codex rate limits: %w", ctxErr)
	}
	if exchangeErr != nil {
		return model.ProviderUsage{}, withStderr("query codex rate limits", exchangeErr, stderr.String())
	}
	if closeErr != nil {
		return model.ProviderUsage{}, closeErr
	}
	if drainErr != nil {
		return model.ProviderUsage{}, fmt.Errorf("drain codex stdout: %w", drainErr)
	}
	if waitErr != nil {
		return model.ProviderUsage{}, withStderr("wait for codex app-server", waitErr, stderr.String())
	}
	return usage, nil
}

func exchange(input io.Reader, output io.Writer) (model.ProviderUsage, error) {
	enc := json.NewEncoder(output)
	dec := json.NewDecoder(input)

	if err := enc.Encode(map[string]any{
		"id":     1,
		"method": "initialize",
		"params": map[string]any{
			"clientInfo": map[string]string{
				"name":    "clock_dashboard",
				"title":   "Clock Dashboard",
				"version": "1.0.0",
			},
		},
	}); err != nil {
		return model.ProviderUsage{}, fmt.Errorf("send codex initialize: %w", err)
	}
	if _, err := readResponse(dec, 1); err != nil {
		return model.ProviderUsage{}, fmt.Errorf("initialize codex app-server: %w", err)
	}

	if err := enc.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return model.ProviderUsage{}, fmt.Errorf("send codex initialized: %w", err)
	}
	if err := enc.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}); err != nil {
		return model.ProviderUsage{}, fmt.Errorf("request codex rate limits: %w", err)
	}

	result, err := readResponse(dec, 2)
	if err != nil {
		return model.ProviderUsage{}, fmt.Errorf("read codex rate limits: %w", err)
	}
	return parseResult(result)
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readResponse(dec *json.Decoder, id int) (json.RawMessage, error) {
	wantID := strconv.Itoa(id)
	for {
		var msg rpcMessage
		if err := dec.Decode(&msg); err != nil {
			return nil, fmt.Errorf("decode app-server message: %w", err)
		}
		if string(bytes.TrimSpace(msg.ID)) != wantID {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		if len(msg.Result) == 0 || bytes.Equal(msg.Result, []byte("null")) {
			return nil, fmt.Errorf("app-server response %d has no result", id)
		}
		return msg.Result, nil
	}
}

type rateLimitsResult struct {
	RateLimits *rateLimitSnapshot `json:"rateLimits"`
}

type rateLimitSnapshot struct {
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
}

type rateLimitWindow struct {
	UsedPercent       *float64 `json:"usedPercent"`
	WindowDurationMin *int64   `json:"windowDurationMins"`
	ResetsAt          *int64   `json:"resetsAt"`
}

func parseResult(raw json.RawMessage) (model.ProviderUsage, error) {
	var result rateLimitsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return model.ProviderUsage{}, fmt.Errorf("decode rate-limit result: %w", err)
	}
	if result.RateLimits == nil {
		return model.ProviderUsage{}, fmt.Errorf("rate-limit result has no main bucket")
	}

	primary, err := toUsageWindow(result.RateLimits.Primary)
	if err != nil {
		return model.ProviderUsage{}, fmt.Errorf("decode primary codex window: %w", err)
	}
	secondary, err := toUsageWindow(result.RateLimits.Secondary)
	if err != nil {
		return model.ProviderUsage{}, fmt.Errorf("decode secondary codex window: %w", err)
	}
	if !primary.Valid && !secondary.Valid {
		return model.ProviderUsage{}, fmt.Errorf("main codex bucket has no usage windows")
	}
	primary, secondary = normalizeWindows(primary, secondary)
	return model.ProviderUsage{
		Updated:   time.Now(),
		Primary:   primary,
		Secondary: secondary,
	}, nil
}

func normalizeWindows(primary, secondary model.UsageWindow) (model.UsageWindow, model.UsageWindow) {
	var fiveHour model.UsageWindow
	var weekly model.UsageWindow
	for _, window := range []model.UsageWindow{primary, secondary} {
		switch window.Duration {
		case fiveHourWindowDuration:
			fiveHour = window
		case weeklyWindowDuration:
			weekly = window
		}
	}

	if primary.Valid && primary.Duration == 0 {
		fiveHour = primary
	}
	if secondary.Valid && secondary.Duration == 0 {
		weekly = secondary
	}
	return fiveHour, weekly
}

func toUsageWindow(window *rateLimitWindow) (model.UsageWindow, error) {
	if window == nil {
		return model.UsageWindow{}, nil
	}
	if window.UsedPercent == nil {
		return model.UsageWindow{}, fmt.Errorf("usedPercent is missing")
	}
	if *window.UsedPercent < 0 || *window.UsedPercent > 100 {
		return model.UsageWindow{}, fmt.Errorf("usedPercent out of range: %v", *window.UsedPercent)
	}

	var duration time.Duration
	if window.WindowDurationMin != nil {
		duration = time.Duration(*window.WindowDurationMin) * time.Minute
	}
	var resetAt time.Time
	if window.ResetsAt != nil {
		resetAt = time.Unix(*window.ResetsAt, 0)
	}
	return model.UsageWindow{
		Utilization: *window.UsedPercent / 100,
		Duration:    duration,
		ResetAt:     resetAt,
		Valid:       true,
	}, nil
}

func closeError(action string, closer io.Closer) error {
	if err := closer.Close(); err != nil {
		if errors.Is(err, os.ErrClosed) {
			return nil
		}
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func withStderr(action string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w; stderr: %s", action, err, stderr)
}

type cappedBuffer struct {
	buf bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := maxStderrBytes - b.buf.Len()
	if remaining > 0 {
		if _, err := b.buf.Write(p[:min(len(p), remaining)]); err != nil {
			return 0, fmt.Errorf("buffer codex stderr: %w", err)
		}
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}
