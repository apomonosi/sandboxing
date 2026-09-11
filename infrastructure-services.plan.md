# Infrastructure Services (DNS, NTP, DHCP) — Design Notes

**Status:** Brainstorm/design discussion, plus one **finding about shipped
code** that should be verified against a live Incus daemon (see "The gap
this exposes"). Nothing here has been implemented and no decision has been
made on the open questions at the bottom. This file preserves the
discussion so it can be picked up later without re-deriving it.

## The question

Basic services — DNS and time — sit awkwardly between agentctl's two
network-control mechanisms. Should they be handled by the firewall (the
network ACL / pf anchor) or by the egress proxy described in
`proxy.plan.md`?

## Short answer: firewall, never proxy

Neither DNS (UDP/53) nor NTP (UDP/123) can traverse an HTTP proxy at all.
A CONNECT proxy only tunnels TCP streams it is explicitly asked to open by
hostname; it has no way to carry a UDP datagram protocol. So by
elimination, both are firewall/ACL concerns.

But the two behave very differently once that's established.

### DNS — the proxy largely removes the need for it

With a CONNECT proxy, the guest sends `CONNECT pypi.org:443` and the
*proxy* resolves the name. The guest never resolves target domains itself.
So under `proxy.plan.md`'s Mode B, guest DNS egress shrinks toward zero,
and that is a security win in its own right: DNS is the classic
exfiltration channel a sandbox otherwise leaves wide open (arbitrary data
encoded into subdomain labels of an attacker-controlled zone, carried out
by a resolver that every firewall policy permits). Point the proxy at a
literal IP rather than a hostname and outbound 53 can plausibly be denied
outright.

What still needs to work regardless is *local* resolution — `localhost`,
the systemd-resolved stub at 127.0.0.53, the guest's own hostname. That is
loopback traffic, not egress, so no policy needs to permit it.

### NTP — don't allow it; use hypervisor time sync

Both backends already get time from the host without any network access:

- **Incus VMs**: `incus-agent` plus the paravirtual clock (kvm-clock);
  guest time tracks the host, including across suspend/resume.
- **Lima/vz**: Apple's Virtualization.framework provides guest time
  synchronization; the QEMU backend has the equivalent via RTC/kvm-clock.

Time correctness genuinely matters here and shouldn't be waved away — a
skewed clock breaks TLS certificate validity windows (connections fail
with confusing "certificate not yet valid" errors) and any TOTP the agent
might use. But the fix is host-synced time, not opening UDP/123 to the
internet. Where an org mandates an internal NTP server, that is a narrow
firewall exception to one specific internal address — never proxied, and
never a blanket UDP/123 allow.

### DHCP — needed, and easy to forget

The guest cannot obtain an address at all without UDP 67/68. This is
rarely noticed because it usually happens before any ACL is in force, but
it belongs in the same tier as the two above: infrastructure the sandbox
needs in order to function, distinct from the traffic the sandbox's agent
generates.

## The gap this exposes in shipped code

There is currently **no handling of DNS, NTP, or DHCP anywhere in the
codebase** — the only DNS references are agentctl resolving allow-domains
host-side (`resolveAllowRules`), which is a different thing entirely.
Meanwhile `ApplyNetworkPolicy` is called unconditionally from
`internal/provider/incus/incus.go:98` in `Create()`, so *every* instance
gets an ACL attached via `config device override <name> eth0
security.acls=<acl>`, which switches Incus's unmatched-egress default to
reject. Two consequences:

1. **The allowlist structurally depends on guest DNS that nothing
   permits.** When `curl https://pypi.org` runs in the guest, curl must
   resolve `pypi.org` itself before connecting. agentctl's IP-based allow
   rule permits the resulting *connection*; the UDP/53 lookup that
   discovers that IP is a separate packet with no matching rule. Worse,
   `networkACLCommands` (`internal/provider/incus/translate.go`) hardcodes
   `protocol=tcp` on every allow rule, so UDP/53 cannot currently be
   expressed at all.

2. **`--deny-lan` blocks the resolver by construction.** The guest's
   resolver lives at an RFC1918 address — Incus's `incusbr0` dnsmasq
   gateway, or Lima's vzNAT/user-mode gateway — squarely inside the
   `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` ranges the deny-lan
   rules reject. The documented Lima pf procedure in
   `internal/provider/lima/capability.go` has exactly the same problem,
   with no exception carved out for the resolver.

**Honest uncertainty:** Incus's bridge ACL implementation *may* implicitly
permit DHCP and DNS to the bridge's own dnsmasq (OVN networks do something
along these lines). That cannot be confirmed without a live daemon — and
that is precisely the point:

- `TestIntegration_ApplyNetworkPolicy_BlocksLAN` proves the *deny* works.
  No test proves the guest is still **functional** after a policy is
  applied. A totally broken-DNS guest passes the entire current suite.
- The bootstrap script masks it further: `bootstrap-user.sh`'s
  `apt-get update && apt-get install -y sudo` path only runs on images
  that lack sudo, so most images silently succeed and never exercise
  guest egress during `create`.

The cheapest way to settle this is one integration test that asserts a
default-policy instance can still resolve a name — the functional
counterpart to the existing blocks-LAN test.

## Proposed model: a distinct infrastructure tier

Treat these as a third category, separate from both the domain allowlist
and deny-lan, evaluated *before* the LAN block, and **computed by agentctl
rather than typed by users** (the guest's resolver address is discoverable
from the backend; asking a user to look it up would be the same
error-prone manual step the pf workaround already suffers from):

| Service | Default | Mechanism |
|---|---|---|
| DNS | allow UDP+TCP/53 **to the resolver address only** | ACL/pf exception, or denied outright when a proxy is configured |
| DHCP | allow UDP 67/68 | ACL/pf exception |
| NTP | **off** — rely on hypervisor time sync | explicit opt-in to a specific internal server when an org requires it |

The important property: keeping these as narrow host+port exceptions
rather than widening `--deny-lan` means "the sandbox may talk to its own
resolver" and "the sandbox may reach the LAN" stay different claims. A
capability table that reports deny-lan as enforced while the policy
quietly permits an entire RFC1918 range would be exactly the kind of
dishonest reporting the capability model exists to prevent.

## Relationship to other in-flight brainstorms

- **`proxy.plan.md`** — Mode B (proxy narrows the ACL to a single
  "allow only proxy IP:port" rule) interacts directly with this: it is
  what makes denying outbound DNS *entirely* realistic, since the proxy
  resolves on the guest's behalf. If both land, the infrastructure tier in
  proxy mode collapses to DHCP plus optional internal NTP.
- **`lima-pf-firewall.plan.md`** — whatever generates the pf anchor must
  emit these exceptions too, or the generated rules will have the same
  resolver-blocking defect the current static `Plan` text does. Tier 1
  there (dynamically generated, correct-for-this-instance commands) is
  also the natural place to fix the Lima half of the finding above.

## Open questions to resolve before this becomes an implementation plan

1. Does Incus's bridge ACL implementation implicitly allow DHCP/DNS to the
   bridge's own dnsmasq? This determines whether the finding above is a
   live bug or a latent one, and must be answered against a real daemon
   before anything is built.
2. How is the resolver address discovered per backend — parse the bridge
   config (`incus network get incusbr0 ipv4.address`), read the guest's
   own `/etc/resolv.conf` after first boot, or something else? They differ
   in whether they need the instance running.
3. Should `networkACLCommands` grow UDP support generally (a `protocol`
   field on `AllowRule`), or just special-case the infrastructure tier?
   The former is more honest but widens the policy surface users can
   express.
4. When a proxy *is* configured, is denying outbound 53 the default, or
   opt-in? Denying it is the stronger posture but will break any tool in
   the guest that resolves names for its own reasons before consulting the
   proxy.
5. Does the infrastructure tier belong in the profile schema at all (an
   admin might want to pin a corporate resolver), or should it stay
   entirely agentctl-computed and invisible?

## Where this fits once resolved

The finding is separable from the design: verifying and fixing guest DNS
under the default policy is a bug fix that should not wait on the broader
infrastructure-tier design. The tier itself should go through the same
plan-mode workflow as the Lima backend and mount features — explore
existing patterns (`networkACLCommands` in
`internal/provider/incus/translate.go`, `resolveAllowRules` in
`internal/provider/incus/incus.go`, `internal/profile/schema.go`), design
a concrete file-by-file plan, and get it reviewed before any code is
written.
