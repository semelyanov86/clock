#!/bin/sh
# Re-resolve the wg0 peer endpoint (a MyFRITZ! hostname) so the tunnel follows
# the home connection's dynamic address. WireGuard does not re-resolve on its
# own; the German ISP rotates the addresses roughly daily, which otherwise
# freezes the tunnel (and the clock display) until a manual restart.
#
# Install to /usr/local/sbin/wg-reresolve-wg0.sh (root:root, 0755) and drive it
# with wg-reresolve.timer. See README.md ("Деплой и WireGuard").
set -eu
IFACE=wg0
CONF="/etc/wireguard/${IFACE}.conf"
[ -e "/sys/class/net/${IFACE}" ] || exit 0

# Strip ONLY the leading "Key =" prefix. The base64 PublicKey itself ends in a
# '=' pad char, so a greedy 's/.*=//' consumes the whole key and yields an empty
# peer -- the bug that turned this script into a silent no-op and left the
# tunnel dead after an IPv6 rotation. 's/^[^=]*=//' removes just the first
# key=value separator and keeps the value (incl. the key's trailing '=' pad).
PEER=$(grep -E '^[[:space:]]*PublicKey' "$CONF" | head -1 | sed -E 's/^[^=]*=[[:space:]]*//')
EP=$(grep -E '^[[:space:]]*Endpoint' "$CONF" | head -1 | sed -E 's/^[^=]*=[[:space:]]*//')

if [ -z "$PEER" ] || [ -z "$EP" ]; then
	echo "wg-reresolve: could not parse PublicKey/Endpoint from $CONF" >&2
	exit 1
fi

# Prefer the hostname's IPv4 (A) record. Left to itself, wg picks the AAAA
# record first, which ties the tunnel to the server's own IPv6 -- after the
# 2026-10-22 reboot that route broke and the device was unreachable for a day
# while IPv4 to the same FRITZ!Box handshook in a second. Fall back to the
# hostname (wg's own resolution) when there is no A record or EP is a literal.
HOST=${EP%:*}
PORT=${EP##*:}
ADDR=$(getent ahostsv4 "$HOST" | awk 'NR == 1 { print $1 }')
TARGET="$EP"
[ -n "$ADDR" ] && TARGET="${ADDR}:${PORT}"

# Do not swallow failures: a non-zero exit surfaces in the systemd journal.
if ! wg set "$IFACE" peer "$PEER" endpoint "$TARGET"; then
	echo "wg-reresolve: 'wg set' failed for endpoint $TARGET ($EP)" >&2
	exit 1
fi
