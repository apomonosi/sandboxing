#!/usr/bin/env bash
# Read-only diagnostic for "my agentctl sandbox has no network" on the
# Incus backend. Changes nothing; every finding prints the command that
# produced it so you can re-run it by hand.
#
# Usage: scripts/diagnose-incus-network.sh <instance-name>
#
# Walks the layers from the outside in, because the causes that look like
# an agentctl policy bug are usually host-side:
#   1. is an ACL even attached
#   2. does the guest have an address
#   3. is the bridge configured for NAT
#   4. firewalld (the usual culprit on Fedora/RHEL)
#   5. host IP forwarding
#   6. does anything actually route out
set -uo pipefail

INSTANCE="${1:-}"
if [ -z "$INSTANCE" ]; then
    echo "usage: $0 <instance-name>" >&2
    exit 2
fi

pass() { printf '  \033[32mok\033[0m    %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; }
warn() { printf '  \033[33mwarn\033[0m  %s\n' "$1"; }
info() { printf '        %s\n' "$1"; }
head_() { printf '\n\033[1m%s\033[0m\n' "$1"; }

need() { command -v "$1" >/dev/null 2>&1; }

if ! need incus; then
    echo "incus not on PATH" >&2
    exit 2
fi

head_ "0. Instance"
if ! incus info "$INSTANCE" >/dev/null 2>&1; then
    fail "instance '$INSTANCE' not found (incus info $INSTANCE)"
    exit 1
fi
STATE=$(incus list "$INSTANCE" -c s -f csv 2>/dev/null)
TYPE=$(incus list "$INSTANCE" -c t -f csv 2>/dev/null)
info "state=$STATE type=$TYPE"
[ "$STATE" = "RUNNING" ] || warn "not running; start it first or the guest checks below will all fail"
if [ "$TYPE" = "VIRTUAL-MACHINE" ]; then
    info "note: 'incus exec' on a VM goes over vsock, not the network — exec"
    info "      working does NOT mean the guest has connectivity."
fi

head_ "1. Is a network ACL attached?"
ACLS=$(incus config show "$INSTANCE" --expanded 2>/dev/null | grep -E 'security\.acls' || true)
if [ -z "$ACLS" ]; then
    pass "no security.acls on the NIC — agentctl's policy is NOT in play"
else
    warn "ACL attached:$ACLS"
    info "rules: incus network acl show agentctl-$INSTANCE"
    info "to rule it out: incus config device unset $INSTANCE eth0 security.acls"
fi

head_ "2. Does the guest have addresses?"
ADDRS=$(incus exec "$INSTANCE" -- ip -br addr 2>/dev/null || true)
if [ -z "$ADDRS" ]; then
    warn "could not read addresses (guest agent not up yet?)"
else
    echo "$ADDRS" | sed 's/^/        /'
    if echo "$ADDRS" | grep -qE 'inet 10\.|inet 192\.168\.|inet 172\.'; then
        pass "has an IPv4 address"
    else
        fail "NO IPv4 address"
        info "IPv6 configures via router advertisements, IPv4 needs DHCP — so"
        info "'IPv6 only' is the signature of DHCP being blocked. See step 4."
    fi
fi

head_ "3. Bridge configuration"
NET=$(incus config show "$INSTANCE" --expanded 2>/dev/null | awk '/network:/{print $2; exit}')
NET="${NET:-incusbr0}"
info "network: $NET"
for key in ipv4.address ipv4.nat ipv6.address ipv6.nat; do
    val=$(incus network get "$NET" "$key" 2>/dev/null || true)
    info "  $key = ${val:-<unset>}"
done
NAT=$(incus network get "$NET" ipv4.nat 2>/dev/null || true)
case "$NAT" in
    true|"") pass "ipv4.nat not disabled" ;;
    *) fail "ipv4.nat=$NAT — outbound traffic will not be masqueraded" ;;
esac
GW=$(incus network get "$NET" ipv4.address 2>/dev/null | cut -d/ -f1 || true)

head_ "4. firewalld (the usual cause on Fedora/RHEL)"
if ! need firewall-cmd; then
    info "firewalld not installed — not the problem here"
elif ! systemctl is-active --quiet firewalld 2>/dev/null; then
    pass "firewalld installed but not running"
else
    ZONE=$(firewall-cmd --get-zone-of-interface="$NET" 2>/dev/null || true)
    if [ "$ZONE" = "trusted" ]; then
        pass "$NET is in the trusted zone"
    elif [ -z "$ZONE" ] || [ "$ZONE" = "no zone" ]; then
        fail "$NET is in NO explicit zone, so it falls into the default zone"
        info "That default is usually 'public', which blocks DHCP (udp/67,68)"
        info "to the bridge's dnsmasq. Fix:"
        info "  sudo firewall-cmd --zone=trusted --change-interface=$NET --permanent"
        info "  sudo firewall-cmd --reload"
    else
        fail "$NET is in zone '$ZONE' (not trusted) — this blocks DHCP/DNS"
        info "  sudo firewall-cmd --zone=trusted --change-interface=$NET --permanent"
        info "  sudo firewall-cmd --reload"
    fi
fi

head_ "5. Host IP forwarding"
FWD=$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null || echo "?")
if [ "$FWD" = "1" ]; then
    pass "net.ipv4.ip_forward=1"
else
    fail "net.ipv4.ip_forward=$FWD — nothing routes off the bridge"
    info "  sudo sysctl -w net.ipv4.ip_forward=1"
fi

head_ "6. Can the guest actually reach anything?"
if [ -n "${GW:-}" ]; then
    if incus exec "$INSTANCE" -- ping -c1 -W2 "$GW" >/dev/null 2>&1; then
        pass "guest can ping the bridge gateway ($GW)"
    else
        fail "guest cannot ping the bridge gateway ($GW)"
    fi
fi
if incus exec "$INSTANCE" -- getent hosts example.com >/dev/null 2>&1; then
    pass "guest resolves DNS"
else
    fail "guest cannot resolve DNS"
    info "resolver config in the guest:"
    incus exec "$INSTANCE" -- cat /etc/resolv.conf 2>/dev/null | sed 's/^/          /' || true
fi
CODE=$(incus exec "$INSTANCE" -- curl -4 -sS -m 8 -o /dev/null -w '%{http_code}' https://example.com 2>/dev/null || true)
if [ "$CODE" = "200" ]; then
    pass "guest reached https://example.com over IPv4"
else
    fail "guest could not reach https://example.com over IPv4 (got '${CODE:-no response}')"
fi

head_ "7. Narrowing further"
info "Is it VM-specific, or the whole bridge? Launch a throwaway *container*"
info "on the same network and compare — containers use a veth, VMs a tap:"
info "  incus launch images:fedora/44 nettest"
info "  incus exec nettest -- getent hosts example.com"
info "  incus delete -f nettest"
info ""
info "  container works, VM doesn't -> VM-side (guest NetworkManager, agent)"
info "  both fail                   -> host-side (firewalld, NAT, forwarding)"
