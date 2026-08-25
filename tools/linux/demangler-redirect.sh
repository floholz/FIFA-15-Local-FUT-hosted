#!/usr/bin/env bash
# Redirect FIFA 15's DirtySDK demangler (ProtoMangle) UDP :10000 to our server via
# kernel DNAT. The kernel's conntrack reverses the reply's source automatically, so
# the game accepts it (a userspace hook can't fix the reply source on the game's
# connected/overlapped socket). Run on every client machine (needs root).
#
#   sudo tools/linux/demangler-redirect.sh on  <server-ip>
#   sudo tools/linux/demangler-redirect.sh off <server-ip>
#
# <server-ip> is the FUT server address the clients use (e.g. the VPS / tailnet IP).
set -euo pipefail

ACTION="${1:-}"; SERVER="${2:-}"
[[ "$ACTION" == "on" || "$ACTION" == "off" ]] || { echo "usage: $0 <on|off> <server-ip>"; exit 2; }
[[ -n "$SERVER" ]] || { echo "usage: $0 <on|off> <server-ip>"; exit 2; }
[[ $EUID -eq 0 ]] || { echo "run with sudo"; exit 2; }

RULE=(-t nat OUTPUT -p udp --dport 10000 -j DNAT --to-destination "$SERVER:10000")

if [[ "$ACTION" == "on" ]]; then
  # avoid duplicates
  iptables "${RULE[@]/OUTPUT/-C OUTPUT}" 2>/dev/null && { echo "already on"; exit 0; } || true
  iptables -t nat -A OUTPUT -p udp --dport 10000 -j DNAT --to-destination "$SERVER:10000"
  echo "demangler :10000 -> $SERVER:10000 (DNAT added)"
else
  iptables -t nat -D OUTPUT -p udp --dport 10000 -j DNAT --to-destination "$SERVER:10000" 2>/dev/null \
    && echo "DNAT removed" || echo "no rule to remove"
fi
