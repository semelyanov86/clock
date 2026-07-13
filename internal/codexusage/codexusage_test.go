package codexusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExchangeReadsPrimaryCodexLimits(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{"userAgent":"clock/0.1"}}`,
		`{"method":"remoteControl/status/changed","params":{"status":"disabled"}}`,
		`{"id":2,"result":{"rateLimits":{"limitId":"codex",` +
			`"primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1783863003},` +
			`"secondary":{"usedPercent":3,"windowDurationMins":10080,"resetsAt":1784449803}},` +
			`"rateLimitsByLimitId":{"codex_bengalfox":{"primary":{"usedPercent":99}}}}}`,
	}, "\n"))
	var output bytes.Buffer

	got, err := exchange(input, &output)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !got.Available() || !got.Primary.Valid || !got.Secondary.Valid {
		t.Fatalf(
			"valid flags = provider:%v primary:%v secondary:%v",
			got.Available(),
			got.Primary.Valid,
			got.Secondary.Valid,
		)
	}
	if got.Primary.Utilization != 0.25 || got.Secondary.Utilization != 0.03 {
		t.Errorf("utilization = %v / %v, want 0.25 / 0.03", got.Primary.Utilization, got.Secondary.Utilization)
	}
	if got.Primary.Duration != 5*time.Hour || got.Secondary.Duration != 7*24*time.Hour {
		t.Errorf("duration = %v / %v, want 5h / 168h", got.Primary.Duration, got.Secondary.Duration)
	}
	if got.Primary.ResetAt.Unix() != 1783863003 || got.Secondary.ResetAt.Unix() != 1784449803 {
		t.Errorf("reset = %v / %v", got.Primary.ResetAt, got.Secondary.ResetAt)
	}
	if got.Updated.IsZero() {
		t.Error("Updated not set")
	}

	messages := decodeMessages(t, output.Bytes())
	if len(messages) != 3 {
		t.Fatalf("sent %d messages, want 3: %s", len(messages), output.String())
	}
	methodsMatch := messages[0]["method"] == "initialize" &&
		messages[1]["method"] == "initialized" &&
		messages[2]["method"] == "account/rateLimits/read"
	if !methodsMatch {
		t.Errorf("unexpected protocol messages: %#v", messages)
	}
}

func TestExchangeAllowsMissingSecondaryWindow(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{}}`,
		`{"id":2,"result":{"rateLimits":{"limitId":"codex",` +
			`"primary":{"usedPercent":8,"windowDurationMins":300,"resetsAt":1783863003},` +
			`"secondary":null}}}`,
	}, "\n"))

	got, err := exchange(input, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !got.Available() || !got.Primary.Valid {
		t.Fatalf("primary usage should be valid: %+v", got)
	}
	if got.Secondary.Valid {
		t.Errorf("secondary usage should be invalid: %+v", got.Secondary)
	}
}

func TestExchangeMapsWeeklyPrimaryToWeeklyUsage(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{}}`,
		`{"id":2,"result":{"rateLimits":{"limitId":"codex",` +
			`"primary":{"usedPercent":0,"windowDurationMins":10080,"resetsAt":1784527511},` +
			`"secondary":null,"planType":"prolite"}}}`,
	}, "\n"))

	got, err := exchange(input, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if got.Primary.Valid {
		t.Errorf("five-hour usage should be invalid: %+v", got.Primary)
	}
	if !got.Secondary.Valid {
		t.Fatalf("weekly usage should be valid: %+v", got.Secondary)
	}
	if got.Secondary.Duration != 7*24*time.Hour {
		t.Errorf("weekly duration = %v, want 168h", got.Secondary.Duration)
	}
	if got.Secondary.Utilization != 0 {
		t.Errorf("weekly utilization = %v, want 0", got.Secondary.Utilization)
	}
	if got.Secondary.ResetAt.Unix() != 1784527511 {
		t.Errorf("weekly reset = %v", got.Secondary.ResetAt)
	}
}

func TestExchangeReturnsRPCError(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{}}`,
		`{"id":2,"error":{"code":-32000,"message":"not logged in"}}`,
	}, "\n"))

	_, err := exchange(input, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("exchange error = %v, want RPC message", err)
	}
}

func TestExchangeRejectsMissingMainBucket(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{}}`,
		`{"id":2,"result":{"rateLimits":null,"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":50}}}}}`,
	}, "\n"))

	_, err := exchange(input, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "main bucket") {
		t.Fatalf("exchange error = %v, want missing main bucket", err)
	}
}

func TestExchangeRejectsWindowWithoutUsedPercent(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(strings.Join([]string{
		`{"id":1,"result":{}}`,
		`{"id":2,"result":{"rateLimits":{"primary":{"windowDurationMins":300}}}}`,
	}, "\n"))

	_, err := exchange(input, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "usedPercent is missing") {
		t.Fatalf("exchange error = %v, want missing usedPercent", err)
	}
}

func TestFetchWithFakeProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	t.Parallel()

	binary := writeFakeCodex(t, `
read -r initialize
printf '%s\n' '{"id":1,"result":{"userAgent":"fake"}}'
read -r initialized
read -r limits
printf '%s\n' \
'{"id":2,"result":{"rateLimits":'\
'{"primary":{"usedPercent":19,"windowDurationMins":300,"resetsAt":1783863003},'\
'"secondary":{"usedPercent":4,"windowDurationMins":10080,"resetsAt":1784449803}}}}'
printf '%s\n' '{"method":"account/rateLimits/updated","params":{}}'
`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	usage, err := New(binary).Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if usage.Primary.Utilization != 0.19 || usage.Secondary.Utilization != 0.04 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestFetchReportsNonZeroExitAndStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	t.Parallel()

	binary := writeFakeCodex(t, `
read -r initialize
printf '%s\n' '{"id":1,"result":{}}'
read -r initialized
read -r limits
printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":7}}}}'
printf '%s\n' 'simulated app-server failure' >&2
exit 7
`)

	_, err := New(binary).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "simulated app-server failure") {
		t.Fatalf("Fetch error = %v, want stderr context", err)
	}
}

func TestFetchCancellationStopsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	t.Parallel()

	binary := writeFakeCodex(t, `
while :; do
    :
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := New(binary).Fetch(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("cancelled Fetch returned after %v", elapsed)
	}
}

func TestFetchCapsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses /bin/sh")
	}
	t.Parallel()

	binary := writeFakeCodex(t, `
i=0
while [ "$i" -lt 2000 ]; do
    printf '%s' '0123456789012345678901234567890123456789' >&2
    i=$((i + 1))
done
exit 9
`)

	_, err := New(binary).Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch succeeded, want process error")
	}
	if got := len(err.Error()); got > maxStderrBytes+512 {
		t.Errorf("error length = %d, want bounded stderr", got)
	}
}

func TestFetchLive(t *testing.T) {
	if os.Getenv("CODEX_USAGE_LIVE") != "1" {
		t.Skip("set CODEX_USAGE_LIVE=1 to query the installed Codex CLI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	usage, err := New(os.Getenv("CODEX_BIN")).Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !usage.Available() {
		t.Fatalf("live usage is incomplete: %+v", usage)
	}
	t.Logf(
		"Codex usage: primary=%.0f%% secondary=%.0f%%",
		usage.Primary.Utilization*100,
		usage.Secondary.Utilization*100,
	)
}

func decodeMessages(t *testing.T, data []byte) []map[string]any {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(data))
	messages := []map[string]any{}
	for dec.More() {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages = append(messages, msg)
	}
	return messages
}

func writeFakeCodex(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "codex")
	content := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write fake Codex: %v", err)
	}
	return path
}
