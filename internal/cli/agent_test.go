package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// capturingProvider records the InstanceSpec passed to Create, so tests
// can assert on fields (DefaultUser, merged allowlist) fake.Provider's
// Instance doesn't itself expose.
type capturingProvider struct {
	*fake.Provider
	lastCreateSpec provider.InstanceSpec
}

func (c *capturingProvider) Create(ctx context.Context, spec provider.InstanceSpec) (*provider.Instance, error) {
	c.lastCreateSpec = spec
	return c.Provider.Create(ctx, spec)
}

func TestCLI_Create_UnknownAgent_Errors(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--agent=bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown --agent value")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("error %q should mention \"unknown agent\"", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error %q should list valid agent names", err)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created when --agent validation fails")
	}
	_ = out
}

func TestCLI_Create_WithAgent_SetsDefaultUserAndMergesAllowlist(t *testing.T) {
	useIsolatedConfig(t)
	p := &capturingProvider{Provider: fake.New()}

	if _, err := executeWith(t, p, "create", "demo", "--image=ubuntu", "--agent=claude"); err != nil {
		t.Fatalf("create --agent=claude: %v", err)
	}

	if p.lastCreateSpec.DefaultUser != "claude" {
		t.Errorf("DefaultUser = %q, want %q", p.lastCreateSpec.DefaultUser, "claude")
	}

	var domains []string
	for _, rule := range p.lastCreateSpec.Overrides.Allow {
		domains = append(domains, rule.Domain)
	}
	// Concrete hosts, not "*.anthropic.com": a wildcard resolves to the
	// apex only, so it allowed the marketing site while the API endpoint
	// stayed blocked. See TestRegistry_NoWildcardDomains in internal/agent.
	for _, want := range []string{"claude.ai", "api.anthropic.com"} {
		found := false
		for _, d := range domains {
			if d == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("merged allowlist %v missing %q", domains, want)
		}
	}
}

func TestCLI_Create_WithAgent_StartsAndInstalls(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu", "--agent=claude")
	if err != nil {
		t.Fatalf("create --agent=claude: %v (%s)", err, out)
	}
	if !strings.Contains(out, "installing claude") {
		t.Errorf("output %q should mention installing the agent", out)
	}

	ctx := context.Background()
	inst, err := p.Status(ctx, "demo")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if inst.Status != provider.StatusRunning {
		t.Errorf("instance status = %q, want running (create --agent implies start)", inst.Status)
	}

	pending, err := p.PendingAgentInstall(ctx, "demo")
	if err != nil {
		t.Fatalf("PendingAgentInstall: %v", err)
	}
	if pending != "" {
		t.Errorf("PendingAgentInstall = %q, want \"\" (install should have completed)", pending)
	}
}

func TestCLI_Create_NoAgent_NeverTouchesAgentMachinery(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	out, err := execute(t, p, "create", "demo", "--image=ubuntu")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	if strings.Contains(out, "installing") {
		t.Errorf("plain create output %q should not mention installing anything", out)
	}
	inst, err := p.Status(context.Background(), "demo")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if inst.Status != provider.StatusStopped {
		t.Errorf("plain create should leave the instance stopped, got %q", inst.Status)
	}
}

func TestCLI_Start_RetriesPendingAgentInstall(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	ctx := context.Background()

	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Simulate a prior `create --agent=codex` whose install never finished
	// (e.g. a transient network failure).
	if err := p.SetAgentRequested(ctx, "demo", "codex"); err != nil {
		t.Fatalf("SetAgentRequested: %v", err)
	}

	out, err := execute(t, p, "start", "demo")
	if err != nil {
		t.Fatalf("start: %v (%s)", err, out)
	}
	if !strings.Contains(out, "retrying incomplete install of codex") {
		t.Errorf("output %q should mention retrying the pending install", out)
	}
	if !strings.Contains(out, "installing codex") {
		t.Errorf("output %q should mention installing codex", out)
	}

	pending, err := p.PendingAgentInstall(ctx, "demo")
	if err != nil {
		t.Fatalf("PendingAgentInstall: %v", err)
	}
	if pending != "" {
		t.Errorf("PendingAgentInstall = %q, want \"\" after a successful retry", pending)
	}
}

func TestCLI_Start_NoAgentRequested_IsNoOp(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()

	if _, err := execute(t, p, "create", "demo", "--image=ubuntu"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err := execute(t, p, "start", "demo")
	if err != nil {
		t.Fatalf("start: %v (%s)", err, out)
	}
	if strings.Contains(out, "installing") || strings.Contains(out, "retrying") {
		t.Errorf("plain start output %q should not mention agent install machinery", out)
	}
}
