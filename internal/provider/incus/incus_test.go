package incus

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// fakeRunner records every command it's asked to run and returns
// canned, empty-but-successful results, so incus.go's real dispatch
// methods can be exercised without a real `incus` binary.
type fakeRunner struct {
	calls [][]string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(args) > 0 && args[0] == "list" {
		// Enough to satisfy Status()/List()'s JSON parsing so callers
		// that chain a status query (e.g. Create's trailing lookup)
		// succeed against this fake.
		return []byte(`[{"name":"demo","status":"Running"}]`), nil, nil
	}
	return []byte("[]"), nil, nil
}

func (f *fakeRunner) RunStream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return 0, nil
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
		if _, err := p.Exec(ctx, "demo", opts); err != nil {
			t.Fatalf("Exec: %v", err)
		}
		want := append([]string{binary}, p.PreviewExec("demo", opts)[0].Args...)
		assertLastCall(t, fr, want)
	})

	t.Run("Shell", func(t *testing.T) {
		fr := &fakeRunner{}
		p := NewWithRunner(fr).(*Provider)
		if err := p.Shell(ctx, "demo", provider.ShellOptions{}); err != nil {
			t.Fatalf("Shell: %v", err)
		}
		want := append([]string{binary}, p.PreviewShell("demo")[0].Args...)
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
