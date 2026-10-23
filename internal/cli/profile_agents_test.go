package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider/fake"
)

// writeProfile writes a minimal profile called name into dir, with an
// agents line when agents is non-empty.
func writeProfile(t *testing.T, dir, name, agents string) {
	t.Helper()
	body := "apiVersion: agentctl.dev/v1\nkind: Profile\nmetadata:\n  name: " + name +
		"\nspec:\n  network:\n    denyLAN: true\n"
	if agents != "" {
		body += "  agents: " + agents + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func profileAgentsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeProfile(t, dir, "pi-local", "[pi]")
	writeProfile(t, dir, "research", "[claude, pi]")
	writeProfile(t, dir, "open", "")
	return dir
}

func TestCLI_Create_ProfileAgents_MatchingPairSucceeds(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	out, err := execute(t, p, "--profile-dir", profileAgentsDir(t), "create", "demo", "--image=ubuntu", "--profile=pi-local", "--agent=pi")
	if err != nil {
		t.Fatalf("create --profile=pi-local --agent=pi: %v (%s)", err, out)
	}
	if _, err := p.Status(context.Background(), "demo"); err != nil {
		t.Errorf("instance should exist: %v", err)
	}
}

func TestCLI_Create_ProfileAgents_MismatchFailsAndCreatesNothing(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	for _, extra := range [][]string{nil, {"--preview"}} {
		args := append([]string{"--profile-dir", profileAgentsDir(t), "create", "demo", "--image=ubuntu", "--profile=pi-local", "--agent=claude"}, extra...)
		out, err := execute(t, p, args...)
		if err == nil {
			t.Fatalf("%v: expected an error for a mismatched profile and agent", extra)
		}
		if want := `profile "pi-local" is for agents [pi], not "claude"`; err.Error() != want {
			t.Errorf("%v: error = %q, want %q", extra, err, want)
		}
		if out != "" {
			t.Errorf("%v: nothing should be printed before the refusal, got %q", extra, out)
		}
		if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
			t.Fatalf("%v: instance should not have been created", extra)
		}
	}
}

func TestCLI_Create_ProfileAgents_NamesFirstRejectingProfile(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	_, err := execute(t, p, "--profile-dir", profileAgentsDir(t), "create", "demo", "--image=ubuntu",
		"--profile=research", "--profile=pi-local", "--agent=claude")
	if want := `profile "pi-local" is for agents [pi], not "claude"`; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	if _, statusErr := p.Status(context.Background(), "demo"); statusErr == nil {
		t.Error("instance should not have been created")
	}
}

func TestCLI_Create_ProfileAgents_NoAgentIsAccepted(t *testing.T) {
	useIsolatedConfig(t)
	p := fake.New()
	out, err := execute(t, p, "--profile-dir", profileAgentsDir(t), "create", "demo", "--image=ubuntu", "--profile=pi-local")
	if err != nil {
		t.Fatalf("create --profile=pi-local without --agent: %v (%s)", err, out)
	}
	if strings.Contains(out, "is for agents") {
		t.Errorf("no --agent should print nothing about the profile's agents, got %q", out)
	}
}

func TestCLI_Create_ProfileAgents_UndeclaredAcceptsAnyAgent(t *testing.T) {
	for _, name := range []string{"claude", "pi", "codex"} {
		t.Run(name, func(t *testing.T) {
			useIsolatedConfig(t)
			p := fake.New()
			if out, err := execute(t, p, "--profile-dir", profileAgentsDir(t), "create", "demo", "--image=ubuntu", "--profile=open", "--agent="+name); err != nil {
				t.Fatalf("create --profile=open --agent=%s: %v (%s)", name, err, out)
			}
		})
	}
}

func TestCLI_Create_ProfileAgents_UnknownNameFailsToLoad(t *testing.T) {
	useIsolatedConfig(t)
	dir := t.TempDir()
	writeProfile(t, dir, "typo", "[cluade]")
	_, err := execute(t, fake.New(), "--profile-dir", dir, "create", "demo", "--image=ubuntu", "--profile=typo")
	if err == nil || !strings.Contains(err.Error(), `unknown agent "cluade" (valid: claude, codex, cursor, gemini, opencode, pi)`) {
		t.Errorf("error = %v, want the unknown agent named with the valid set", err)
	}
}
