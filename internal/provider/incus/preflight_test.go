package incus

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preflightRunner fakes the two binaries Preflight shells out to, so the
// checks can be exercised without a firewalld or an Incus daemon.
type preflightRunner struct {
	firewalldRunning bool
	networksJSON     string
	// zones maps an interface name to what `firewall-cmd
	// --get-zone-of-interface` prints for it.
	zones map[string]string
	// zoneOnStderr routes the zone answer to stderr instead of stdout,
	// since firewall-cmd is not consistent about which stream it uses for
	// the unzoned case.
	zoneOnStderr bool
	// zoneExitsNonZero mirrors the real behavior for an unzoned
	// interface: it prints "no zone" *and* exits non-zero.
	zoneExitsNonZero bool
}

func (r *preflightRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	switch {
	case name == "firewall-cmd" && len(args) == 1 && args[0] == "--state":
		if !r.firewalldRunning {
			return nil, []byte("not running"), errors.New("exit status 252")
		}
		return []byte("running"), nil, nil

	case name == "firewall-cmd" && len(args) == 1 && strings.HasPrefix(args[0], "--get-zone-of-interface="):
		iface := strings.TrimPrefix(args[0], "--get-zone-of-interface=")
		zone, ok := r.zones[iface]
		if !ok {
			return nil, nil, errors.New("exit status 2")
		}
		var err error
		if r.zoneExitsNonZero {
			err = errors.New("exit status 2")
		}
		if r.zoneOnStderr {
			return nil, []byte(zone + "\n"), err
		}
		return []byte(zone + "\n"), nil, err

	case name == binary && len(args) >= 2 && args[0] == "network" && args[1] == "list":
		return []byte(r.networksJSON), nil, nil
	}
	return nil, nil, nil
}

func (r *preflightRunner) RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return 0, nil
}

const oneManagedBridgeJSON = `[
  {"name":"incusbr0","type":"bridge","managed":true},
  {"name":"eth0","type":"physical","managed":false},
  {"name":"unmanagedbr","type":"bridge","managed":false}
]`

// pointIPForwardAt redirects the procfs read at a fixture so these tests
// don't depend on the host's own forwarding setting.
func pointIPForwardAt(t *testing.T, contents string) {
	t.Helper()
	orig := ipForwardPath
	path := filepath.Join(t.TempDir(), "ip_forward")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	ipForwardPath = path
	t.Cleanup(func() { ipForwardPath = orig })
}

// The case this whole check exists for: firewalld running, bridge
// unzoned. The real firewall-cmd prints "no zone" *and* exits non-zero,
// so a check that trusted the exit code would miss exactly the situation
// worth reporting.
func TestPreflight_WarnsOnUnzonedBridge(t *testing.T) {
	pointIPForwardAt(t, "1\n")
	for _, tc := range []struct {
		name            string
		stderr, nonZero bool
	}{
		{"stdout, exit 0", false, false},
		{"stdout, exit non-zero", false, true},
		{"stderr, exit non-zero", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewWithRunner(&preflightRunner{
				firewalldRunning: true,
				networksJSON:     oneManagedBridgeJSON,
				zones:            map[string]string{"incusbr0": "no zone"},
				zoneOnStderr:     tc.stderr,
				zoneExitsNonZero: tc.nonZero,
			}).(*Provider)

			warnings := p.Preflight(context.Background())
			if len(warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly one about the bridge zone", warnings)
			}
			if !strings.Contains(warnings[0], "incusbr0") || !strings.Contains(warnings[0], "--zone=trusted") {
				t.Errorf("warning should name the bridge and the fix, got %q", warnings[0])
			}
		})
	}
}

func TestPreflight_SilentWhenBridgeIsTrusted(t *testing.T) {
	pointIPForwardAt(t, "1\n")
	p := NewWithRunner(&preflightRunner{
		firewalldRunning: true,
		networksJSON:     oneManagedBridgeJSON,
		zones:            map[string]string{"incusbr0": "trusted"},
	}).(*Provider)

	if warnings := p.Preflight(context.Background()); len(warnings) != 0 {
		t.Errorf("warnings = %v, want none for a correctly zoned bridge", warnings)
	}
}

// Every "can't tell" path must stay quiet. A preflight that cries wolf
// gets ignored, which costs more than not having one.
func TestPreflight_SilentWhenItCannotTell(t *testing.T) {
	pointIPForwardAt(t, "1\n")
	for _, tc := range []struct {
		name   string
		runner *preflightRunner
	}{
		{"firewalld not running or not installed", &preflightRunner{
			firewalldRunning: false,
			networksJSON:     oneManagedBridgeJSON,
			zones:            map[string]string{"incusbr0": "no zone"},
		}},
		{"zone query fails outright (usually needs root)", &preflightRunner{
			firewalldRunning: true,
			networksJSON:     oneManagedBridgeJSON,
			zones:            map[string]string{}, // no answer for any interface
		}},
		{"no managed bridges", &preflightRunner{
			firewalldRunning: true,
			networksJSON:     `[{"name":"eth0","type":"physical","managed":false}]`,
			zones:            map[string]string{"incusbr0": "no zone"},
		}},
		{"network list unparseable", &preflightRunner{
			firewalldRunning: true,
			networksJSON:     `not json`,
			zones:            map[string]string{"incusbr0": "no zone"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewWithRunner(tc.runner).(*Provider)
			if warnings := p.Preflight(context.Background()); len(warnings) != 0 {
				t.Errorf("warnings = %v, want silence when the check can't tell", warnings)
			}
		})
	}
}

// Unmanaged bridges are none of agentctl's business — it didn't create
// them and doesn't put instances on them.
func TestPreflight_IgnoresUnmanagedNetworks(t *testing.T) {
	pointIPForwardAt(t, "1\n")
	p := NewWithRunner(&preflightRunner{
		firewalldRunning: true,
		networksJSON:     oneManagedBridgeJSON,
		zones: map[string]string{
			"incusbr0":    "trusted",
			"unmanagedbr": "no zone",
		},
	}).(*Provider)

	if warnings := p.Preflight(context.Background()); len(warnings) != 0 {
		t.Errorf("warnings = %v, want none: only the managed bridge matters", warnings)
	}
}

func TestCheckIPForward(t *testing.T) {
	pointIPForwardAt(t, "0\n")
	warnings := checkIPForward()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ip_forward") {
		t.Fatalf("warnings = %v, want one about ip_forward being 0", warnings)
	}

	pointIPForwardAt(t, "1\n")
	if warnings := checkIPForward(); len(warnings) != 0 {
		t.Errorf("warnings = %v, want none when forwarding is enabled", warnings)
	}

	// Absent procfs (non-Linux, or unreadable) tells us nothing.
	ipForwardPath = filepath.Join(t.TempDir(), "does-not-exist")
	if warnings := checkIPForward(); len(warnings) != 0 {
		t.Errorf("warnings = %v, want silence when the file can't be read", warnings)
	}
}
