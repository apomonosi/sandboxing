package egress

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyPermits(t *testing.T) {
	pol := Policy{Allow: []AllowRule{
		{Domain: "pypi.org", Ports: []int{443}},
		{Domain: "*.anthropic.com", Ports: []int{443}},
		{Domain: "any-port.example", Ports: nil},
		{Domain: "93.184.216.34", Ports: []int{80}},
		{Domain: "2001:db8::1"},
	}}
	if err := pol.validateAndNormalize(); err != nil {
		t.Fatalf("validateAndNormalize: %v", err)
	}
	cases := []struct {
		host string
		port int
		want bool
	}{
		{"pypi.org", 443, true},
		{"pypi.org", 80, false},                // wrong port
		{"files.pythonhosted.org", 443, false}, // not listed
		{"evilpypi.org", 443, false},           // suffix of a non-wildcard rule must not match
		{"api.anthropic.com", 443, true},       // wildcard subdomain
		{"a.b.anthropic.com", 443, true},       // nested subdomain
		{"anthropic.com", 443, true},           // wildcard covers the apex (Incus parity)
		{"anthropic.com.evil.net", 443, false}, // not a subdomain
		{"evilanthropic.com", 443, false},      // label boundary respected
		{"any-port.example", 22, true},         // no ports = any port
		{"93.184.216.34", 80, true},            // IP-literal rule
		{"93.184.216.34", 443, false},          // ...with its port restriction
		{"2001:db8::1", 443, true},             // IPv6 literal, any port
		{"203.0.113.9", 443, false},            // unlisted IP literal
	}
	for _, c := range cases {
		if got := pol.permits(normalizeHost(c.host), c.port); got != c.want {
			t.Errorf("permits(%q, %d) = %v, want %v", c.host, c.port, got, c.want)
		}
	}
}

func TestPolicyPermitsLiteral(t *testing.T) {
	pol := Policy{DenyLAN: true, Allow: []AllowRule{
		{Domain: "10.20.30.40", Ports: []int{8080}},
		{Domain: "[fd00::5]", Ports: []int{443}},
		{Domain: "192.168.1.9"}, // no ports: never an explicit LAN opening
		{Domain: "lanhost.example", Ports: []int{8080}},
		{Domain: "*.corp.example", Ports: []int{8080}},
	}}
	if err := pol.validateAndNormalize(); err != nil {
		t.Fatalf("validateAndNormalize: %v", err)
	}
	cases := []struct {
		host string
		port int
		want bool
	}{
		{"10.20.30.40", 8080, true},      // literal rule, listed port
		{"10.20.30.40", 22, false},       // wrong port
		{"[fd00::5]", 443, true},         // IPv6 literal in brackets
		{"192.168.1.9", 80, false},       // literal rule without ports
		{"lanhost.example", 8080, false}, // domain rule
		{"a.corp.example", 8080, false},  // wildcard rule
		{"10.20.30.41", 8080, false},     // unlisted literal
	}
	for _, c := range cases {
		if got := pol.permitsLiteral(normalizeHost(c.host), c.port); got != c.want {
			t.Errorf("permitsLiteral(%q, %d) = %v, want %v", c.host, c.port, got, c.want)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"PyPI.org.":            "pypi.org",
		" Example.COM ":        "example.com",
		"[2001:DB8::1]":        "2001:db8::1",
		"::ffff:10.0.0.1":      "10.0.0.1",
		"*.Anthropic.com":      "*.anthropic.com",
		"2001:0db8:0000::0001": "2001:db8::1",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlockedReason(t *testing.T) {
	denyLAN := Policy{DenyLAN: true}
	allowLAN := Policy{DenyLAN: false}
	cases := []struct {
		ip           string
		blockedDeny  bool // with DenyLAN
		blockedAllow bool // without DenyLAN
	}{
		{"93.184.216.34", false, false},
		{"2606:4700::1111", false, false},
		// Always blocked: these reach the host the proxy runs on.
		{"127.0.0.1", true, true},
		{"127.8.9.10", true, true},
		{"::1", true, true},
		{"0.0.0.0", true, true},
		{"0.1.2.3", true, true},
		{"::", true, true},
		{"224.0.0.1", true, true},
		{"ff02::1", true, true},
		{"255.255.255.255", true, true},
		{"::ffff:127.0.0.1", true, true},
		// LAN: blocked only with DenyLAN — the same four ranges Incus rejects...
		{"10.1.2.3", true, false},
		{"172.16.0.1", true, false},
		{"172.31.255.255", true, false},
		{"192.168.1.1", true, false},
		{"169.254.169.254", true, false},
		// ...plus their IPv6 counterparts, and LAN addresses in disguise.
		{"fd00::1", true, false},
		{"fe80::1", true, false},
		{"::ffff:192.168.1.1", true, false},
		{"64:ff9b::a00:1", true, false},      // NAT64 for 10.0.0.1
		{"64:ff9b::7f00:1", true, true},      // NAT64 for 127.0.0.1
		{"64:ff9b::5db8:d822", false, false}, // NAT64 for 93.184.216.34
		// Just outside the LAN ranges.
		{"172.32.0.1", false, false},
		{"11.0.0.1", false, false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := denyLAN.blockedReason(ip, false) != ""; got != c.blockedDeny {
			t.Errorf("DenyLAN: blocked(%s) = %v, want %v (reason %q)", c.ip, got, c.blockedDeny, denyLAN.blockedReason(ip, false))
		}
		if got := allowLAN.blockedReason(ip, false) != ""; got != c.blockedAllow {
			t.Errorf("allow-LAN: blocked(%s) = %v, want %v (reason %q)", c.ip, got, c.blockedAllow, allowLAN.blockedReason(ip, false))
		}
	}
}

// An explicit IP-literal allow rule (see permitsLiteral) opens a private
// address under deny-LAN, and 127.0.0.1 or ::1, and nothing else:
// link-local, other loopback spellings and the other always-refused ranges
// stay refused.
func TestBlockedReason_Explicit(t *testing.T) {
	denyLAN := Policy{DenyLAN: true}
	cases := []struct {
		ip                string
		blockedImplicit   bool
		blockedExplicit   bool
		wantExplicitWords string // substring of the explicit reason, if blocked
	}{
		{"10.20.30.40", true, false, ""},
		{"172.16.0.1", true, false, ""},
		{"192.168.1.1", true, false, ""},
		{"fd00::1", true, false, ""},
		{"::ffff:192.168.1.1", true, false, ""},
		{"169.254.169.254", true, true, "link-local"},
		{"fe80::1", true, true, "link-local"},
		{"127.0.0.1", true, false, ""},
		{"::1", true, false, ""},
		{"::ffff:127.0.0.1", true, false, ""},
		{"127.0.0.2", true, true, "loopback"},
		{"64:ff9b::7f00:1", true, true, "loopback"}, // NAT64 spelling of 127.0.0.1
		{"0.0.0.0", true, true, "unspecified"},
		{"::", true, true, "unspecified"},
		{"224.0.0.1", true, true, "multicast"},
		{"ff02::1", true, true, "multicast"},
		{"255.255.255.255", true, true, "broadcast"},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if got := denyLAN.blockedReason(ip, false) != ""; got != c.blockedImplicit {
			t.Errorf("blocked(%s, explicit=false) = %v, want %v", c.ip, got, c.blockedImplicit)
		}
		r := denyLAN.blockedReason(ip, true)
		if got := r != ""; got != c.blockedExplicit {
			t.Errorf("blocked(%s, explicit=true) = %v, want %v (reason %q)", c.ip, got, c.blockedExplicit, r)
		}
		if !strings.Contains(r, c.wantExplicitWords) {
			t.Errorf("blocked(%s, explicit=true) reason %q, want it to mention %q", c.ip, r, c.wantExplicitWords)
		}
	}
}

// A loopback literal with its ports listed reaches that one host service,
// under either deny-LAN setting; nothing else about loopback opens.
func TestPolicy_LoopbackLiteral(t *testing.T) {
	for _, denyLAN := range []bool{true, false} {
		pol := Policy{DenyLAN: denyLAN, Allow: []AllowRule{
			{Domain: "127.0.0.1", Ports: []int{8081}},
			{Domain: "[::1]", Ports: []int{8081}},
			{Domain: "127.0.0.1"}, // no ports: never an opening
			{Domain: "llama.example", Ports: []int{8081}},
		}}
		if err := pol.validateAndNormalize(); err != nil {
			t.Fatalf("validateAndNormalize: %v", err)
		}
		cases := []struct {
			host string
			port int
			want bool
		}{
			{"127.0.0.1", 8081, true},
			{"[::1]", 8081, true},
			{"::1", 8081, true},
			{"127.0.0.1", 22, false},       // unlisted port, despite the portless rule
			{"[::1]", 22, false},           // likewise for IPv6
			{"llama.example", 8081, false}, // a domain resolving to loopback
		}
		for _, c := range cases {
			h := normalizeHost(c.host)
			explicit := pol.permitsLiteral(h, c.port)
			ip := net.ParseIP(h)
			if ip == nil {
				ip = net.ParseIP("127.0.0.1") // what llama.example resolves to
			}
			reason := pol.blockedReason(ip, explicit)
			if got := pol.permits(h, c.port) && reason == ""; got != c.want {
				t.Errorf("DenyLAN=%v: %s:%d permitted = %v, want %v (reason %q)", denyLAN, c.host, c.port, got, c.want, reason)
			}
			if !c.want && !strings.Contains(reason, "loopback") {
				t.Errorf("DenyLAN=%v: %s:%d reason %q, want it to mention loopback", denyLAN, c.host, c.port, reason)
			}
		}
	}
}

func TestWritePolicy_LoadPolicy_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "egress-policy.json")
	want := Policy{DenyLAN: true, Allow: []AllowRule{{Domain: "PyPI.org", Ports: []int{443}}}}
	if err := WritePolicy(path, want); err != nil {
		t.Fatalf("WritePolicy: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("policy file mode = %o, want 0600", perm)
	}
	got, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("LoadPolicy: %v", err)
	}
	if !got.DenyLAN || len(got.Allow) != 1 || got.Allow[0].Domain != "pypi.org" || got.Allow[0].Ports[0] != 443 {
		t.Errorf("LoadPolicy() = %+v, want the written policy (with its domain normalized)", got)
	}
}

func TestWritePolicy_EmptyAllowIsAnEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	if err := WritePolicy(path, Policy{DenyLAN: true}); err != nil {
		t.Fatalf("WritePolicy: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"allow": []`) {
		t.Errorf("policy file = %s, want an explicit empty allow list", data)
	}
}

// TestLoadPolicy_RejectsDamagedFiles: a security policy must fail loudly
// (and the proxy then denies everything) rather than load as something
// looser than what agentctl wrote.
func TestLoadPolicy_RejectsDamagedFiles(t *testing.T) {
	cases := map[string]string{
		"unknown field": `{"denyLAN": true, "allow": [], "allowAll": true}`,
		"bad port":      `{"denyLAN": true, "allow": [{"domain": "pypi.org", "ports": [70000]}]}`,
		"empty domain":  `{"denyLAN": true, "allow": [{"domain": ""}]}`,
		"not json":      `allow: everything`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "p.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPolicy(path); err == nil {
				t.Errorf("LoadPolicy(%s) succeeded, want an error", content)
			}
		})
	}
}
