#!/bin/sh
# Re-resolve the wg0 peer endpoint (a MyFRITZ! hostname) so the tunnel follows
# the home connection's dynamic IPv6 prefix. WireGuard does not re-resolve on
# its own; the German ISP rotates the prefix roughly daily, which otherwise
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

# Re-set the endpoint from the hostname so wg resolves the current address.
# Do not swallow failures: a non-zero exit surfaces in the systemd journal.
if ! wg set "$IFACE" peer "$PEER" endpoint "$EP"; then
	echo "wg-reresolve: 'wg set' failed for endpoint $EP" >&2
	exit 1
fi
