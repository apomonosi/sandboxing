package incus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// fakeRunner records every command it's asked to run and returns
// canned, empty-but-successful results, so incus.go's real dispatch
// methods can be exercised without a real `incus` binary.
//
// agentFailCount/agentFailAlways let tests simulate the in-guest
// incus-agent not being ready yet: they make the `exec <name> -- true`
// readiness probe (see waitForAgent) fail with the same error text real
// Incus returns, either for a fixed number of calls or indefinitely.
//
// userConfigValue is what a `config get <name> user.agentctl-shell-user`
// call returns — empty by default, matching real Incus's behavior for an
// unset custom config key, which resolveExecUser treats as "fall back to
// root". Tests exercising the non-root path set it explicitly.
type fakeRunner struct {
	calls               [][]string
	agentFailCount      int
	agentFailAlways     bool
	userConfigValue     string
	agentConfigValue    string
	agentInstalledValue string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if isAgentProbe(args) && (f.agentFailAlways || f.agentFailCount > 0) {
		if f.agentFailCount > 0 {
			f.agentFailCount--
		}
		return nil, []byte("Error: " + agentNotRunningMsg), errors.New("exit status 1")
	}
	if len(args) > 0 && args[0] == "list" {
		// Enough to satisfy Status()/List()'s JSON parsing so callers
		// that chain a status query (e.g. Create's trailing lookup)
		// succeed against this fake.
		return []byte(`[{"name":"demo","status":"Running"}]`), nil, nil
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "get" {
		switch args[len(args)-1] {
		case userConfigKey:
			return []byte(f.userConfigValue), nil, nil
		case agentConfigKey:
			return []byte(f.agentConfigValue), nil, nil
		case agentInstalledConfigKey:
			return []byte(f.agentInstalledValue), nil, nil
		}
	}
	return []byte("[]"), nil, nil
}

func isAgentProbe(args []string) bool {
	return len(args) == 4 && args[0] == "exec" && args[2] == "--" && args[3] == "true"
}

func (f *fakeRunner) RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if isBootstrapCall(args) {
		// bootstrapUserCommand(username) = {"sh", "-s", "--", username};
		// report a fixed UID so Create()'s provisionDefaultUser has
		// something to parse, mirroring the real script's last line.
		if w, ok := stdout.(*bytes.Buffer); ok {
			w.WriteString("AGENTCTL_UID=1500\n")
		}
	}
	return 0, nil
}

func isBootstrapCall(args []string) bool {
	return contains(args, "sh") && contains(args, "-s")
}

// TestRealDispatch_MatchesPreview is the guard against preview output
// drifting from what actually executes: for each lifecycle/interactive
// operation, it runs the real Provider method against a fakeRunner and
// asserts the recorded command equals what the corresponding Preview*
// method reports.
func TestRealDispatch_MatchesPreview(t *testing.T) {
	ctx := context.Background()

	t.Run("Start", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		if err := p.Start(ctx, "demo"); err != nil {
			t.Fatalf("Start: %v", err)
		}
		want := append([]string{binary}, p.PreviewStart("demo")[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("Stop", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		opts := provider.StopOptions{Force: true}
		if err := p.Stop(ctx, "demo", opts); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		want := append([]string{binary}, p.PreviewStop("demo", opts)[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("Delete", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		if err := p.Delete(ctx, "demo", true); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		want := append([]string{binary}, p.PreviewDelete("demo", true)[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("Exec", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		opts := provider.ExecOptions{Command: []string{"echo", "hi"}}
		// Compute the expected command via Preview *before* the real
		// dispatch: PreviewExec now also does a (fake) config-get read,
		// and doing it first keeps the real RunStream call as fr.calls'
		// last entry, which assertLastCall relies on.
		wantCmds, err := p.PreviewExec(ctx, "demo", opts)
		if err != nil {
			t.Fatalf("PreviewExec: %v", err)
		}
		if _, err := p.Exec(ctx, "demo", opts); err != nil {
			t.Fatalf("Exec: %v", err)
		}
		want := append([]string{binary}, wantCmds[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("Shell", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		opts := provider.ShellOptions{}
		wantCmds, err := p.PreviewShell(ctx, "demo", opts)
		if err != nil {
			t.Fatalf("PreviewShell: %v", err)
		}
		if err := p.Shell(ctx, "demo", opts); err != nil {
			t.Fatalf("Shell: %v", err)
		}
		want := append([]string{binary}, wantCmds[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("View", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		if err := p.View(ctx, "demo", provider.ViewOptions{}); err != nil {
			t.Fatalf("View: %v", err)
		}
		want := append([]string{binary}, p.PreviewView("demo", provider.ViewOptions{})[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("ImagePull", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		if err := p.ImagePull(ctx, "images:ubuntu/24.04"); err != nil {
			t.Fatalf("ImagePull: %v", err)
		}
		want := append([]string{binary}, p.PreviewImagePull("images:ubuntu/24.04")[0].Args...)
		assertLastCall(t, fr, want)
	})
}

func assertLastCall(t *testing.T, fr *fakeRunner, want []string) {
	t.Helper()
	if len(fr.calls) == 0 {
		t.Fatal("expected at least one recorded call")
	}
	got := fr.calls[len(fr.calls)-1]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("last call = %v, want %v", got, want)
	}
}

// shrinkAgentWait overrides the package-level agent-readiness timing so
// tests exercising waitForAgent don't have to wait out the real 30s
// default, restoring the originals on cleanup.
func shrinkAgentWait(t *testing.T) {
	t.Helper()
	origTimeout, origPoll := agentReadyTimeout, agentPollInterval
	agentReadyTimeout = 100 * time.Millisecond
	agentPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		agentReadyTimeout, agentPollInterval = origTimeout, origPoll
	})
}

func TestExec_WaitsForAgentBeforeRunning(t *testing.T) {
	shrinkAgentWait(t)
	fr := &fakeRunner{agentFailCount: 2}
	p := NewWithRunner(fr).(*Provider)
	var stderr bytes.Buffer
	opts := provider.ExecOptions{Command: []string{"echo", "hi"}, Stderr: &stderr}

	code, err := p.Exec(context.Background(), "demo", opts)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "waiting for the VM agent") {
		t.Errorf("stderr = %q, want a heads-up about waiting for the agent", stderr.String())
	}
	// The real exec must have actually run, as the last recorded call.
	want := append([]string{binary}, buildExecArgs("demo", opts.Command, nil)...)
	assertLastCall(t, fr, want)
}

func TestShell_WaitsForAgentBeforeRunning(t *testing.T) {
	shrinkAgentWait(t)
	fr := &fakeRunner{agentFailCount: 1}
	p := NewWithRunner(fr).(*Provider)

	if err := p.Shell(context.Background(), "demo", provider.ShellOptions{}); err != nil {
		t.Fatalf("Shell: %v", err)
	}
	want := append([]string{binary}, buildShellArgs("demo", nil)...)
	assertLastCall(t, fr, want)
}

func TestExec_AgentNeverReady_TimesOutWithoutRunningCommand(t *testing.T) {
	shrinkAgentWait(t)
	fr := &fakeRunner{agentFailAlways: true}
	p := NewWithRunner(fr).(*Provider)

	_, err := p.Exec(context.Background(), "demo", provider.ExecOptions{Command: []string{"echo", "hi"}})
	if err == nil {
		t.Fatal("expected an error once the agent never becomes ready")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("error = %q, want it to explain the agent timed out", err.Error())
	}
	// The real command must never have been attempted.
	realArgs := buildExecArgs("demo", []string{"echo", "hi"}, nil)
	for _, c := range fr.calls {
		if reflect.DeepEqual(c[1:], realArgs) {
			t.Errorf("real exec command was run despite the agent never becoming ready: %v", c)
		}
	}
}

func TestExec_UnrelatedAgentProbeErrorFailsFast(t *testing.T) {
	fr := &unrelatedErrorRunner{}
	p := NewWithRunner(fr).(*Provider)

	_, err := p.Exec(context.Background(), "demo", provider.ExecOptions{Command: []string{"echo", "hi"}})
	if err == nil {
		t.Fatal("expected the unrelated error to propagate immediately")
	}
	if strings.Contains(err.Error(), "did not become ready") {
		t.Error("an unrelated error must not be reported as an agent-readiness timeout")
	}
	if fr.calls != 1 {
		t.Errorf("expected exactly one probe attempt (fail fast, no retry), got %d", fr.calls)
	}
}

// unrelatedErrorRunner always fails the agent probe with an error that
// has nothing to do with agent readiness, to verify waitForAgent doesn't
// retry (and thus doesn't mask) errors it shouldn't.
type unrelatedErrorRunner struct{ calls int }

func (r *unrelatedErrorRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	r.calls++
	return nil, []byte("Error: instance not found"), errors.New("exit status 1")
}

func (r *unrelatedErrorRunner) RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return 0, nil
}

func TestExec_NonRootUser_UsesResolvedUIDAndHome(t *testing.T) {
	fr := &fakeRunner{userConfigValue: "claude:1500:/home/claude"}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Exec(context.Background(), "demo", provider.ExecOptions{Command: []string{"true"}}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	want := append([]string{binary}, buildExecArgs("demo", []string{"true"}, &execUser{uid: "1500", home: "/home/claude"})...)
	assertLastCall(t, fr, want)
}

func TestExec_RootFlag_SkipsResolvedUser(t *testing.T) {
	fr := &fakeRunner{userConfigValue: "claude:1500:/home/claude"}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Exec(context.Background(), "demo", provider.ExecOptions{Command: []string{"true"}, Root: true}); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	want := append([]string{binary}, buildExecArgs("demo", []string{"true"}, nil)...)
	assertLastCall(t, fr, want)
	for _, c := range fr.calls {
		if len(c) >= 3 && c[0] == "config" && c[1] == "get" {
			t.Errorf("--root should skip the user-config lookup entirely, got call: %v", c)
		}
	}
}

func TestCreate_ProvisionsDefaultUser(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Create(context.Background(), provider.InstanceSpec{Name: "demo", Image: "images:ubuntu/24.04"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var sawBootstrap, sawConfigSet bool
	for _, c := range fr.calls {
		if len(c) >= 2 && c[0] == binary && contains(c, "sh") && contains(c, "agent") {
			sawBootstrap = true
		}
		if len(c) >= 3 && c[1] == "config" && c[2] == "set" {
			sawConfigSet = true
		}
	}
	if !sawBootstrap {
		t.Error("expected Create to run the user-bootstrap script with the default username \"agent\"")
	}
	if !sawConfigSet {
		t.Error("expected Create to record the provisioned user via `incus config set`")
	}
}

func TestCreate_DefaultUser_UsesSpecOverride(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if _, err := p.Create(context.Background(), provider.InstanceSpec{
		Name: "demo", Image: "images:ubuntu/24.04", DefaultUser: "claude",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	found := false
	for _, c := range fr.calls {
		if contains(c, "sh") && contains(c, "claude") {
			found = true
		}
	}
	if !found {
		t.Error("expected Create to bootstrap the DefaultUser (\"claude\") from InstanceSpec, not the generic default")
	}
}

func TestSetAgentRequested_DispatchesConfigSet(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if err := p.SetAgentRequested(context.Background(), "demo", "claude"); err != nil {
		t.Fatalf("SetAgentRequested: %v", err)
	}
	want := []string{binary, "config", "set", "demo", "user.agentctl-agent=claude"}
	assertLastCall(t, fr, want)
}

func TestMarkAgentInstalled_DispatchesConfigSet(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)

	if err := p.MarkAgentInstalled(context.Background(), "demo"); err != nil {
		t.Fatalf("MarkAgentInstalled: %v", err)
	}
	want := []string{binary, "config", "set", "demo", "user.agentctl-agent-installed=true"}
	assertLastCall(t, fr, want)
}

func TestPendingAgentInstall(t *testing.T) {
	ctx := context.Background()

	t.Run("nothing requested", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		got, err := p.PendingAgentInstall(ctx, "demo")
		if err != nil {
			t.Fatalf("PendingAgentInstall: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want \"\" when no agent was ever requested", got)
		}
	})

	t.Run("requested but not installed", func(t *testing.T) {
		fr := &fakeRunner{agentConfigValue: "claude"}
		p := NewWithRunner(fr).(*Provider)
		got, err := p.PendingAgentInstall(ctx, "demo")
		if err != nil {
			t.Fatalf("PendingAgentInstall: %v", err)
		}
		if got != "claude" {
			t.Errorf("got %q, want \"claude\"", got)
		}
	})

	t.Run("requested and installed", func(t *testing.T) {
		fr := &fakeRunner{agentConfigValue: "claude", agentInstalledValue: "true"}
		p := NewWithRunner(fr).(*Provider)
		got, err := p.PendingAgentInstall(ctx, "demo")
		if err != nil {
			t.Fatalf("PendingAgentInstall: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want \"\" once installed", got)
		}
	})
}
