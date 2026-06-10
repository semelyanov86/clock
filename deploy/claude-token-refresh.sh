#!/usr/bin/env bash
# Keep the Claude Code OAuth token fresh by letting the OFFICIAL `claude` CLI
# refresh it shortly before it expires. Driven by claude-token-refresh.timer.
#
# Why the CLI and not an in-process refresh: the platform.claude.com token
# endpoint 429s a hand-rolled refresh request (it appears to distinguish the
# genuine client), but `claude` itself refreshes fine from this host. So we let
# it do the exchange and the clock service just reads the resulting token.
#
# It only invokes `claude` when the token is near expiry, so it costs a
# negligible amount of the subscription quota the widget reports. On failure it
# backs off (15m → 30m → 1h → cap 2h) so a throttled endpoint is not hammered.
set -uo pipefail

CREDS="${CLAUDE_CREDENTIALS_PATH:-$HOME/.claude/.credentials.json}"
CLAUDE_BIN="${CLAUDE_BIN:-$HOME/.local/bin/claude}"
STATE="${CLAUDE_REFRESH_STATE:-$HOME/.claude/.token-refresh.state}"
THRESHOLD="${CLAUDE_REFRESH_THRESHOLD_SECONDS:-1800}" # refresh when <30m to expiry
PROMPT="${CLAUDE_REFRESH_PROMPT:-ok}"

log() { echo "$*"; }

now=$(date +%s)
exp_ms=$(jq -r '.claudeAiOauth.expiresAt // 0' "$CREDS" 2>/dev/null || echo 0)
exp=$(( exp_ms / 1000 ))
remain=$(( exp - now ))

# Healthy and not near expiry: nothing to do (cheap, no quota spent).
if [ "$exp" -gt 0 ] && [ "$remain" -gt "$THRESHOLD" ]; then
	log "token fresh: $(( remain / 60 ))m left; skipping"
	exit 0
fi

# Respect backoff after a previous failure.
next_attempt=0
failures=0
if [ -r "$STATE" ]; then
	read -r next_attempt failures < "$STATE" 2>/dev/null || { next_attempt=0; failures=0; }
fi
if [ "$now" -lt "$next_attempt" ]; then
	log "refresh in backoff for $(( (next_attempt - now) / 60 ))m more; skipping"
	exit 0
fi

log "token near/after expiry ($(( remain / 60 ))m); invoking claude to refresh"
timeout 90 "$CLAUDE_BIN" -p "$PROMPT" >/dev/null 2>&1
claude_rc=$?

new_exp_ms=$(jq -r '.claudeAiOauth.expiresAt // 0' "$CREDS" 2>/dev/null || echo 0)
if [ "$new_exp_ms" -gt "$exp_ms" ]; then
	rm -f "$STATE"
	log "refreshed: expiresAt -> $(date -d "@$(( new_exp_ms / 1000 ))" '+%F %T %Z') (claude rc=$claude_rc)"
	exit 0
fi

# No advance. If the token is still valid, claude simply hasn't refreshed yet —
# not a failure; retry on the next tick as expiry approaches. Only treat it as a
# real failure (and back off) once the token is actually expired.
if [ "$exp" -gt 0 ] && [ "$remain" -gt 0 ]; then
	log "claude did not refresh yet ($(( remain / 60 ))m left, rc=$claude_rc); will retry next tick"
	exit 0
fi

failures=$(( failures + 1 ))
shift_n=$(( failures - 1 )); [ "$shift_n" -gt 3 ] && shift_n=3
delay=$(( 900 << shift_n )); [ "$delay" -gt 7200 ] && delay=7200
printf '%s %s\n' "$(( now + delay ))" "$failures" > "$STATE"
log "refresh failed (claude rc=$claude_rc, expiresAt unchanged); backing off $(( delay / 60 ))m" >&2
exit 1
