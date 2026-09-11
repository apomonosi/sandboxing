package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/apomonosi/sandboxing/internal/provider"
)

var _ provider.Preflighter = (*Provider)(nil)

// ipForwardPath is a var so tests can point it at a fixture instead of
// the real procfs, which is absent on non-Linux and unwritable anyway.
var ipForwardPath = "/proc/sys/net/ipv4/ip_forward"

// Preflight reports host-configuration problems that would make a freshly
// created instance come up without working networking. Every check is
// read-only and every one fails silent: see provider.Preflighter for why
// a false alarm is worse than no check.
func (p *Provider) Preflight(ctx context.Context) []string {
	var warnings []string
	warnings = append(warnings, p.checkFirewalldZones(ctx)...)
	warnings = append(warnings, checkIPForward()...)
	return warnings
}

// checkFirewalldZones warns about any managed bridge that firewalld has
// not put in the trusted zone.
//
// On Fedora/RHEL the bridge is not assigned to a zone at all, so it falls
// through to the default (normally "public"), which blocks DHCP on
// UDP 67/68 to the bridge's own dnsmasq. IPv6 configures anyway via
// router advertisements, so the guest comes up looking half-working: an
// IPv6 address, IPv6 resolvers, and no IPv4.
func (p *Provider) checkFirewalldZones(ctx context.Context) []string {
	// --state exits non-zero when firewalld is absent, stopped, or not
	// queryable by this user. Any of those means there is nothing to say.
	if _, _, err := p.runner.Run(ctx, "firewall-cmd", "--state"); err != nil {
		return nil
	}

	var warnings []string
	for _, bridge := range p.managedBridges(ctx) {
		// Deliberately ignoring the exit code: firewall-cmd exits
		// non-zero for an unzoned interface while still printing
		// "no zone", which is precisely the case worth warning about.
		// The output, not the status, is the signal.
		stdout, stderr, _ := p.runner.Run(ctx, "firewall-cmd", "--get-zone-of-interface="+bridge)
		zone := strings.TrimSpace(string(stdout))
		if zone == "" {
			zone = strings.TrimSpace(string(stderr))
		}
		// Empty means the query itself failed (it often needs root).
		// Silence beats guessing.
		if zone == "" || zone == "trusted" {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"firewalld has %s in zone %q, not \"trusted\", which blocks DHCP to the bridge — "+
				"instances will come up with an IPv6 address, no IPv4, and no working DNS. Fix with:\n"+
				"    sudo firewall-cmd --zone=trusted --change-interface=%s --permanent && sudo firewall-cmd --reload",
			bridge, zone, bridge))
	}
	return warnings
}

// managedBridges lists the bridge networks Incus itself manages, which
// are the only ones whose firewall zone agentctl has any business
// commenting on.
func (p *Provider) managedBridges(ctx context.Context) []string {
	stdout, _, err := p.run(ctx, "network", "list", "--format", "json")
	if err != nil {
		return nil
	}
	var networks []struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Managed bool   `json:"managed"`
	}
	if err := json.Unmarshal(stdout, &networks); err != nil {
		return nil
	}
	var out []string
	for _, n := range networks {
		if n.Managed && n.Type == "bridge" {
			out = append(out, n.Name)
		}
	}
	return out
}

// checkIPForward catches the other way a bridge ends up going nowhere:
// with forwarding off, traffic reaches the bridge and stops there. Incus
// normally enables it, but a hardened host or a conflicting sysctl
// drop-in can put it back.
func checkIPForward() []string {
	data, err := os.ReadFile(ipForwardPath)
	if err != nil {
		return nil // not Linux, or not readable — nothing learned
	}
	if strings.TrimSpace(string(data)) == "1" {
		return nil
	}
	return []string{
		"net.ipv4.ip_forward is 0, so nothing routes off the Incus bridge. Fix with:\n" +
			"    sudo sysctl -w net.ipv4.ip_forward=1",
	}
}
