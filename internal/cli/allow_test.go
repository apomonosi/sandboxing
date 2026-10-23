package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

func ruleFor(rules []provider.AllowRule, domain string) (provider.AllowRule, bool) {
	for _, r := range rules {
		if r.Domain == domain {
			return r, true
		}
	}
	return provider.AllowRule{}, false
}

// "host:80,443" is one entry with two ports. --allow used to be a
// comma-splitting flag, which turned it into host:80 plus a rule for a
// host named "443".
func TestCLI_Create_AllowWithSeveralPorts(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu", "--allow=example.com:80,443"); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := []provider.AllowRule{{Domain: "example.com", Ports: []int{80, 443}}}
	if got := p.lastCreateSpec.Overrides.Allow; !reflect.DeepEqual(got, want) {
		t.Errorf("Allow = %+v, want %+v", got, want)
	}
}

func TestCLI_Create_AllowURL(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu",
		"--allow=https://gitlab.example.com/", "--allow", "http://mirror.example.com:8080"); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := []provider.AllowRule{
		{Domain: "gitlab.example.com", Ports: []int{443}},
		{Domain: "mirror.example.com", Ports: []int{8080}},
	}
	if got := p.lastCreateSpec.Overrides.Allow; !reflect.DeepEqual(got, want) {
		t.Errorf("Allow = %+v, want %+v", got, want)
	}
}

func TestCLI_Create_AllowURLWithPath_ErrorsBeforeCreating(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	_, err := execute(t, p, "create", "demo", "--image=ubuntu", "--allow=https://github.com/org/repo")
	if err == nil {
		t.Fatal("expected an error for an --allow URL with a path")
	}
	if !strings.Contains(err.Error(), "not by URL path") || !strings.Contains(err.Error(), "github.com:443") {
		t.Errorf("error %q should explain host-level filtering and suggest github.com:443", err)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created")
	}
}

func TestCLI_Create_AllowPreset(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu", "--allow-preset=github,pypi", "--allow-preset", "apt"); err != nil {
		t.Fatalf("create: %v", err)
	}
	allow := p.lastCreateSpec.Overrides.Allow
	for _, domain := range []string{"github.com", "release-assets.githubusercontent.com", "pypi.org", "files.pythonhosted.org", "archive.ubuntu.com"} {
		if _, ok := ruleFor(allow, domain); !ok {
			t.Errorf("allowlist %+v is missing %s", allow, domain)
		}
	}
	if r, _ := ruleFor(allow, "archive.ubuntu.com"); !reflect.DeepEqual(r.Ports, []int{80, 443}) {
		t.Errorf("archive.ubuntu.com ports = %v, want [80 443] (apt uses plain HTTP)", r.Ports)
	}
}

func TestCLI_Create_UnknownAllowPreset_ErrorsBeforeCreating(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	_, err := execute(t, p, "create", "demo", "--image=ubuntu", "--allow-preset=pipy")
	if err == nil {
		t.Fatal("expected an error for an unknown preset")
	}
	if !strings.Contains(err.Error(), `"pipy"`) || !strings.Contains(err.Error(), "pypi") {
		t.Errorf("error %q should name the bad preset and list the valid ones", err)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created")
	}
}

func TestCLI_Create_AllowFile(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}
	path := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(path, []byte("# team hosts\nnexus.example.com:8443\nhttps://gitlab.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu", "--allow-file="+path, "--allow=example.com:443"); err != nil {
		t.Fatalf("create: %v", err)
	}
	want := []provider.AllowRule{
		{Domain: "example.com", Ports: []int{443}},
		{Domain: "nexus.example.com", Ports: []int{8443}},
		{Domain: "gitlab.example.com", Ports: []int{443}},
	}
	if got := p.lastCreateSpec.Overrides.Allow; !reflect.DeepEqual(got, want) {
		t.Errorf("Allow = %+v, want %+v", got, want)
	}
}

func TestCLI_Create_BadAllowFile_ErrorsBeforeCreating(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	path := filepath.Join(t.TempDir(), "allow.txt")
	if err := os.WriteFile(path, []byte("ok.example.com\nnot a host\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t, p, "create", "demo", "--image=ubuntu", "--allow-file="+path)
	if err == nil || !strings.Contains(err.Error(), path+":2:") {
		t.Fatalf("err = %v, want one pointing at %s:2", err, path)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created")
	}
}

// A preset alone is an allowlist, so it must hit the same capability gate
// as --allow on a provider that can't enforce one.
func TestCLI_Create_AllowPresetIsGatedLikeAllow(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New().WithCapability(provider.Capability{
		Feature: provider.FeatureNetworkACL, Status: provider.NotAvailable, Message: "no ACLs here",
	})

	_, err := execute(t, p, "create", "demo", "--image=ubuntu", "--allow-preset=pypi")
	if err == nil {
		t.Fatal("expected the network.acl capability gate to block a preset-only allowlist")
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created")
	}
}

func TestCLI_ProfilePresets(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "profile", "presets")
	if err != nil {
		t.Fatalf("profile presets: %v", err)
	}
	for _, want := range []string{"apt:", "github:", "pypi:", "  files.pythonhosted.org:443", "  archive.ubuntu.com:80,443"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile presets output missing %q:\n%s", want, out)
		}
	}

	out, err = execute(t, p, "--output=json", "profile", "presets")
	if err != nil {
		t.Fatalf("profile presets --output=json: %v", err)
	}
	var presets []struct {
		Name  string `json:"name"`
		Allow []struct {
			Domain string `json:"domain"`
			Ports  []int  `json:"ports"`
		} `json:"allow"`
	}
	if err := json.Unmarshal([]byte(out), &presets); err != nil {
		t.Fatalf("parsing JSON output: %v\n%s", err, out)
	}
	if len(presets) == 0 || presets[0].Name != "apk" || presets[0].Allow[0].Domain != "dl-cdn.alpinelinux.org" {
		t.Errorf("unexpected JSON output: %+v", presets)
	}
}

const lanNotice = "agentctl: allow entry 10.20.30.40:8080 is a LAN address; it is reachable on that port only, and deny-LAN stays on for the rest of the network.\n"

// An IP-literal LAN entry with a port is the narrow exception to
// deny-LAN; create says so once, and only when it would.
func TestCLI_Create_LANLiteralNotice(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}
	out, err := executeWith(t, p, "create", "demo", "--image=ubuntu", "--allow=10.20.30.40:8080", "--allow=10.20.30.40:8080")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if n := strings.Count(out, lanNotice); n != 1 {
		t.Errorf("output has the LAN notice %d times, want once:\n%s", n, out)
	}
	if !p.lastCreateSpec.Overrides.DenyLAN {
		t.Error("deny-LAN should stay on")
	}

	for _, args := range [][]string{
		{"--output=json", "create", "demo2", "--image=ubuntu", "--allow=10.20.30.40:8080"},
		{"--preview", "create", "demo3", "--image=ubuntu", "--allow=10.20.30.40:8080"},
		{"create", "demo4", "--image=ubuntu", "--allow=10.20.30.40", "--allow-lan"},
	} {
		out, err := executeWith(t, previewingFake{fake.New()}, args...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", args, err, out)
		}
		if strings.Contains(out, "is a LAN address") {
			t.Errorf("%v printed a LAN notice, want none:\n%s", args, out)
		}
	}
}

func TestCLI_Create_LANLiteralErrors(t *testing.T) {
	useIsolatedConfig(t)
	dir := t.TempDir()
	allowFile := filepath.Join(dir, "allow.txt")
	if err := os.WriteFile(allowFile, []byte("10.20.30.40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--allow=10.20.30.40"}, `allow entry "10.20.30.40" is a LAN address: list the port(s) you need (e.g. 10.20.30.40:8080); deny-LAN permits a LAN host only on listed ports`},
		{[]string{"--allow-file=" + allowFile}, `allow entry "10.20.30.40" is a LAN address`},
		{[]string{"--allow=169.254.169.254:80"}, "link-local"},
		{[]string{"--allow=127.0.0.1"}, `allow entry "127.0.0.1" is a loopback address: list the port(s) of the one host service you need (e.g. 127.0.0.1:8081)`},
		{[]string{"--allow=127.0.0.1", "--allow-lan"}, `allow entry "127.0.0.1" is a loopback address`},
	} {
		p := &capturingProvider{Provider: fake.New()}
		out, err := executeWith(t, p, append([]string{"create", "demo", "--image=ubuntu"}, c.args...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("create %v: error = %v, want it to contain %q (%s)", c.args, err, c.want, out)
		}
		if p.lastCreateSpec.Name != "" {
			t.Errorf("create %v reached the provider, want it refused first", c.args)
		}
	}
}

const loopbackNotice = "agentctl: allow entry 127.0.0.1:8081 is a loopback address; the sandbox can reach that one service on this machine, and nothing else on it; the guest's no_proxy is adjusted so ordinary clients reach it.\n"

// A loopback literal with a port reaches that one host service; create
// says so once, including that the guest's no_proxy is adjusted for it,
// under either deny-LAN setting, and only when it would.
func TestCLI_Create_LoopbackLiteralNotice(t *testing.T) {
	useIsolatedConfig(t)
	for _, extra := range [][]string{nil, {"--allow-lan"}} {
		p := &capturingProvider{Provider: fake.New()}
		args := append([]string{"create", "demo", "--image=ubuntu", "--allow=127.0.0.1:8081", "--allow=127.0.0.1:8081"}, extra...)
		out, err := executeWith(t, p, args...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", args, err, out)
		}
		if n := strings.Count(out, loopbackNotice); n != 1 {
			t.Errorf("%v: output has the loopback notice %d times, want once:\n%s", args, n, out)
		}
		if r, ok := ruleFor(p.lastCreateSpec.Overrides.Allow, "127.0.0.1"); !ok || !reflect.DeepEqual(r.Ports, []int{8081}) {
			t.Errorf("%v: allowlist %+v, want 127.0.0.1:8081 passed through", args, p.lastCreateSpec.Overrides.Allow)
		}
	}

	for _, args := range [][]string{
		{"--output=json", "create", "demo2", "--image=ubuntu", "--allow=127.0.0.1:8081"},
		{"--preview", "create", "demo3", "--image=ubuntu", "--allow=127.0.0.1:8081"},
	} {
		out, err := executeWith(t, previewingFake{fake.New()}, args...)
		if err != nil {
			t.Fatalf("%v: %v (%s)", args, err, out)
		}
		if strings.Contains(out, "is a loopback address") {
			t.Errorf("%v printed a loopback notice, want none:\n%s", args, out)
		}
	}
}

// On Incus a loopback entry would name the guest itself, so create
// refuses it, saying why, before the provider sees anything.
func TestCLI_Create_LoopbackLiteralRefusedOnIncus(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}
	reg := provider.NewRegistry()
	reg.Register("incus", func() (provider.Provider, error) { return p, nil })
	root := NewRootCmd(reg)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--provider", "incus", "create", "demo", "--image=images:ubuntu/24.04", "--allow=127.0.0.1:8081"})
	err := root.Execute()
	want := "allow entry 127.0.0.1:8081 is a loopback address, which the Incus backend refuses: an Incus guest has its own network namespace, so 127.0.0.1 inside it is the guest itself, not this machine."
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "Incus bridge") {
		t.Fatalf("create on incus: error = %v, want it to contain %q and point at the bridge address (%s)", err, want, out.String())
	}
	if p.lastCreateSpec.Name != "" {
		t.Error("create reached the provider, want it refused first")
	}
	if strings.Contains(out.String(), "the sandbox can reach that one service") {
		t.Errorf("printed the Lima loopback notice on Incus:\n%s", out.String())
	}
}
