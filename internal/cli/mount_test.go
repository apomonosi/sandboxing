package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/profile"
	"github.com/apomonosi/sandboxing/internal/provider"
)

func TestParseMountFlag(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want profile.Mount
	}{
		{
			name: "host path only defaults to read-only and an empty guest path",
			in:   "~/src/project",
			want: profile.Mount{HostPath: "~/src/project"},
		},
		{
			name: "writable suffix",
			in:   "~/src/project:w",
			want: profile.Mount{HostPath: "~/src/project", Writable: true},
		},
		{
			name: "explicit read-only suffix",
			in:   "~/src/project:ro",
			want: profile.Mount{HostPath: "~/src/project"},
		},
		{
			name: "explicit guest path",
			in:   "~/src/project:/workspace",
			want: profile.Mount{HostPath: "~/src/project", GuestPath: "/workspace"},
		},
		{
			name: "guest path and writable suffix",
			in:   "~/src/project:/workspace:w",
			want: profile.Mount{HostPath: "~/src/project", GuestPath: "/workspace", Writable: true},
		},
		{
			name: "absolute host path",
			in:   "/home/me/src:/workspace",
			want: profile.Mount{HostPath: "/home/me/src", GuestPath: "/workspace"},
		},
		// The Windows cases are why parsing runs right to left: a naive
		// left-to-right split would read "C" as the host path and treat
		// the rest of the drive-qualified path as a guest path.
		{
			name: "windows host path alone",
			in:   `C:\Users\me\src`,
			want: profile.Mount{HostPath: `C:\Users\me\src`},
		},
		{
			name: "windows host path with guest path and suffix",
			in:   `C:\Users\me\src:/workspace:w`,
			want: profile.Mount{HostPath: `C:\Users\me\src`, GuestPath: "/workspace", Writable: true},
		},
		{
			name: "windows host path with writable suffix only",
			in:   `C:\Users\me\src:w`,
			want: profile.Mount{HostPath: `C:\Users\me\src`, Writable: true},
		},
		{
			// A relative guest path isn't a guest path: it has no leading
			// slash, so it stays part of the host path and ResolveMounts
			// is what ultimately rejects it.
			name: "relative second element is not treated as a guest path",
			in:   "~/src:workspace",
			want: profile.Mount{HostPath: "~/src:workspace"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMountFlag(tc.in)
			if err != nil {
				t.Fatalf("parseMountFlag(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseMountFlag(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMountFlag_Errors(t *testing.T) {
	for _, in := range []string{"", "   ", ":w", ":/workspace:w"} {
		if _, err := parseMountFlag(in); err == nil {
			t.Errorf("parseMountFlag(%q) = nil error, want an error", in)
		}
	}
}

func TestMountFlags_Apply(t *testing.T) {
	base := profile.Policy{Mounts: []profile.Mount{
		{HostPath: "~/agentctl/workspaces/demo", GuestPath: "/workspace", Writable: true},
	}}

	t.Run("mount accumulates on the profile's mounts", func(t *testing.T) {
		got, err := (&mountFlags{mounts: []string{"~/src:/src"}}).apply(base)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if len(got.Mounts) != 2 {
			t.Fatalf("apply() produced %d mounts, want 2 (profile + flag)", len(got.Mounts))
		}
	})

	t.Run("mount-only replaces the profile's mounts", func(t *testing.T) {
		got, err := (&mountFlags{only: []string{"~/src:/src"}}).apply(base)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if len(got.Mounts) != 1 || got.Mounts[0].HostPath != "~/src" {
			t.Errorf("apply() = %+v, want only the flag's mount", got.Mounts)
		}
	})

	t.Run("mount-none drops everything", func(t *testing.T) {
		got, err := (&mountFlags{none: true, mounts: []string{"~/src:/src"}}).apply(base)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if len(got.Mounts) != 0 {
			t.Errorf("apply() = %+v, want no mounts", got.Mounts)
		}
	})

	t.Run("mount-writable upgrades every surviving mount", func(t *testing.T) {
		got, err := (&mountFlags{writable: true, mounts: []string{"~/src:/src"}}).apply(base)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		for _, m := range got.Mounts {
			if !m.Writable {
				t.Errorf("apply() left %s read-only, want writable", m.HostPath)
			}
		}
	})

	// apply must not write through to the profile's own slice: create
	// resolves a policy once and a mutation here would leak into any later
	// use of it.
	t.Run("does not mutate the input policy", func(t *testing.T) {
		if _, err := (&mountFlags{writable: true}).apply(base); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if _, err := (&mountFlags{mounts: []string{"~/src:/src"}}).apply(base); err != nil {
			t.Fatalf("apply: %v", err)
		}
		if len(base.Mounts) != 1 {
			t.Fatalf("apply() mutated the input policy: %d mounts, want 1", len(base.Mounts))
		}
	})
}

func TestRenderMounts_ReportsEmptySetExplicitly(t *testing.T) {
	var buf bytes.Buffer
	renderMounts(&buf, nil, false)
	if !strings.Contains(buf.String(), "no host directories") {
		t.Errorf("renderMounts(nil) = %q, want it to say no directories are exposed", buf.String())
	}

	buf.Reset()
	renderMounts(&buf, []provider.Mount{
		{HostPath: "/home/me/src", GuestPath: "/workspace", Writable: true},
		{HostPath: "/home/me/docs", GuestPath: "/docs"},
	}, false)
	out := buf.String()
	for _, want := range []string{"/home/me/src -> /workspace (writable)", "/home/me/docs -> /docs (read-only)"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderMounts() = %q, want it to contain %q", out, want)
		}
	}
}

// The mount listing is commentary printed alongside a command's real
// output, so it must stay out of the way of --output=json entirely — the
// instance's own JSON already carries the mount set.
func TestRenderMounts_SilentUnderJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	renderMounts(&buf, []provider.Mount{{HostPath: "/home/me/src", GuestPath: "/workspace"}}, true)
	if buf.Len() != 0 {
		t.Errorf("renderMounts(jsonOutput=true) wrote %q, want nothing", buf.String())
	}
}
