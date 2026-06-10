#!/usr/bin/env bash
# Keep the Claude Code OAuth token fresh by letting the OFFICIAL `claude` CLI
# refresh it the moment it expires. Run as a long-lived systemd service
# (claude-token-refresh.service): it sleeps until the token's expiry, wakes,
# lets `claude` do the refresh, then sleeps again until the next expiry. No
# polling — `claude` is invoked at most once per token lifetime (~8h), so it
# spends a negligible amount of the subscription quota the widget reports.
#
# Why the CLI and not an in-process refresh: the platform.claude.com token
# endpoint 429s a hand-rolled refresh (it appears to single out the genuine
# client), but `claude` refreshes fine from this host. `claude` only refreshes
# an expired / about-to-expire token, never a healthy one, so we wake at expiry.
set -uo pipefail

CREDS="${CLAUDE_CREDENTIALS_PATH:-$HOME/.claude/.credentials.json}"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"
PROMPT="${CLAUDE_REFRESH_PROMPT:-ok}"
GRACE="${CLAUDE_REFRESH_GRACE_SECONDS:-5}" # wake this long after expiry
MAX_SLEEP="${CLAUDE_REFRESH_MAX_SLEEP:-21600}" # re-check at least every 6h (clock-skew safety)
MIN_BACKOFF=900  # 15m
MAX_BACKOFF=7200 # 2h

read_exp_ms() { jq -r '.claudeAiOauth.expiresAt // 0' "$CREDS" 2>/dev/null || echo 0; }

fails=0
while true; do
	exp_ms=$(read_exp_ms)
	exp=$(( exp_ms / 1000 ))
	now=$(date +%s)
	wait=$(( exp - now + GRACE ))

	# Token still valid: sleep until just past its expiry (capped, so an external
	# refresh or a clock change is picked up within MAX_SLEEP).
	if [ "$exp_ms" -gt 0 ] && [ "$wait" -gt 0 ]; then
		[ "$wait" -gt "$MAX_SLEEP" ] && wait="$MAX_SLEEP"
		echo "token valid for $(( (exp - now) / 60 ))m; sleeping ${wait}s"
		sleep "$wait"
		continue
	fi

	# Expired (or expiry unknown): let claude refresh it.
	echo "token expired; invoking claude to refresh"
	timeout 90 "$CLAUDE_BIN" -p "$PROMPT" >/dev/null 2>&1
	rc=$?
	new_ms=$(read_exp_ms)
	if [ "$new_ms" -gt "$exp_ms" ]; then
		fails=0
		echo "refreshed: expiresAt -> $(date -d "@$(( new_ms / 1000 ))" '+%F %T %Z') (claude rc=$rc)"
		continue
	fi

	# Refresh did not take: back off (15m → 30m → 1h → cap 2h) so a throttled
	# endpoint is not hammered, then retry.
	fails=$(( fails + 1 ))
	sh=$(( fails - 1 )); [ "$sh" -gt 3 ] && sh=3
	back=$(( MIN_BACKOFF << sh )); [ "$back" -gt "$MAX_BACKOFF" ] && back="$MAX_BACKOFF"
	echo "refresh failed (claude rc=$rc, expiresAt unchanged); retrying in $(( back / 60 ))m" >&2
	sleep "$back"
done
