package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points os.UserHomeDir at a temporary directory so the
// home-relative rules (the $HOME denial, alwaysDenied, "~" expansion) can
// be exercised without touching the developer's real home.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func allow(b bool) *bool { return &b }

func TestResolveMounts_ExpandsHomeAndInstanceName(t *testing.T) {
	home := withHome(t)

	got, err := ResolveMounts(
		[]Mount{{HostPath: "~/agentctl/workspaces/{{.Name}}", GuestPath: "/workspace", Writable: true}},
		"demo",
		MountPolicy{CreateMissing: true},
	)
	if err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ResolveMounts returned %d mounts, want 1", len(got))
	}

	want, err := filepath.EvalSymlinks(filepath.Join(home, "agentctl", "workspaces", "demo"))
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got[0].HostPath != want {
		t.Errorf("HostPath = %q, want %q", got[0].HostPath, want)
	}
	if !got[0].Writable {
		t.Error("Writable = false, want true")
	}
}

// The built-in profiles' workspace path cannot exist before first use, so
// createMissing is what makes them work at all — and the directory must
// not be world-readable, since it holds whatever the agent writes.
func TestResolveMounts_CreateMissing(t *testing.T) {
	home := withHome(t)
	target := filepath.Join(home, "agentctl", "workspaces", "demo")

	if _, err := ResolveMounts(
		[]Mount{{HostPath: target, GuestPath: "/workspace"}},
		"demo",
		MountPolicy{CreateMissing: true},
	); err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat created directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("created directory mode = %o, want 700", perm)
	}
}

func TestResolveMounts_MissingWithoutCreateMissingIsAnError(t *testing.T) {
	home := withHome(t)
	_, err := ResolveMounts(
		[]Mount{{HostPath: filepath.Join(home, "nope"), GuestPath: "/workspace"}},
		"demo",
		MountPolicy{},
	)
	if err == nil {
		t.Fatal("ResolveMounts = nil error for a missing host path, want an error")
	}
}

// A symlink is the obvious way to smuggle a path past allowedRoots, so
// canonicalization has to happen before containment is checked, not after.
func TestResolveMounts_SymlinkCannotEscapeAllowedRoots(t *testing.T) {
	home := withHome(t)
	allowed := filepath.Join(home, "src")
	secret := filepath.Join(home, "secret")
	for _, dir := range []string{allowed, secret} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}
	escape := filepath.Join(allowed, "escape")
	if err := os.Symlink(secret, escape); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := ResolveMounts(
		[]Mount{{HostPath: escape, GuestPath: "/workspace"}},
		"demo",
		MountPolicy{AllowedRoots: []string{allowed}},
	)
	if err == nil {
		t.Fatal("ResolveMounts allowed a symlink pointing outside allowedRoots, want an error")
	}
	if !strings.Contains(err.Error(), "allowedRoots") {
		t.Errorf("error = %v, want it to mention allowedRoots", err)
	}
}

// Containment must compare path segments: a string-prefix check would let
// "/home/u/srcevil" pass as being inside "/home/u/src".
func TestResolveMounts_AllowedRootBoundaryIsSegmentWise(t *testing.T) {
	home := withHome(t)
	allowed := filepath.Join(home, "src")
	sibling := filepath.Join(home, "srcevil")
	for _, dir := range []string{allowed, sibling} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}

	if _, err := ResolveMounts(
		[]Mount{{HostPath: sibling, GuestPath: "/workspace"}},
		"demo",
		MountPolicy{AllowedRoots: []string{allowed}},
	); err == nil {
		t.Error("ResolveMounts accepted /srcevil under allowed root /src, want an error")
	}

	if _, err := ResolveMounts(
		[]Mount{{HostPath: filepath.Join(allowed, "project"), GuestPath: "/workspace"}},
		"demo",
		MountPolicy{AllowedRoots: []string{allowed}, CreateMissing: true},
	); err != nil {
		t.Errorf("ResolveMounts rejected a genuine child of the allowed root: %v", err)
	}
}

// Decision 4: $HOME itself is refused by default. Lima's stock templates
// mount the whole home directory, which is precisely what agentctl must
// not do — but a path *inside* $HOME (the per-instance workspace) stays
// legal, or the built-in profiles would not work.
func TestResolveMounts_HomeDirectory(t *testing.T) {
	home := withHome(t)

	_, err := ResolveMounts([]Mount{{HostPath: home, GuestPath: "/host"}}, "demo", MountPolicy{})
	if err == nil {
		t.Fatal("ResolveMounts mounted $HOME by default, want an error")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error = %v, want it to explain the home-directory refusal", err)
	}

	if _, err := ResolveMounts(
		[]Mount{{HostPath: home, GuestPath: "/host"}}, "demo",
		MountPolicy{AllowHome: allow(true)},
	); err != nil {
		t.Errorf("ResolveMounts with allowHome: %v, want it permitted", err)
	}

	if _, err := ResolveMounts(
		[]Mount{{HostPath: filepath.Join(home, "projects", "app"), GuestPath: "/workspace"}},
		"demo", MountPolicy{CreateMissing: true},
	); err != nil {
		t.Errorf("ResolveMounts rejected a directory inside $HOME: %v", err)
	}
}

// alwaysDenied exists so a profile author cannot forget these. The
// provider-state entries matter most: write access to ~/.lima or Incus's
// own state lets an agent rewrite the next sandbox's configuration.
func TestResolveMounts_AlwaysDeniedPaths(t *testing.T) {
	home := withHome(t)
	for _, rel := range []string{".ssh", ".aws", ".lima", ".local/share/incus", ".config/agentctl"} {
		t.Run(rel, func(t *testing.T) {
			dir := filepath.Join(home, rel)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if _, err := ResolveMounts(
				[]Mount{{HostPath: dir, GuestPath: "/mnt"}}, "demo",
				MountPolicy{AllowHome: allow(true)},
			); err == nil {
				t.Errorf("ResolveMounts mounted %s, want it always denied", rel)
			}
		})
	}
}

func TestResolveMounts_DenyPaths(t *testing.T) {
	home := withHome(t)
	denied := filepath.Join(home, "src", "secrets")
	if err := os.MkdirAll(denied, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_, err := ResolveMounts(
		[]Mount{{HostPath: denied, GuestPath: "/workspace"}}, "demo",
		MountPolicy{DenyPaths: []string{"~/src/secrets"}},
	)
	if err == nil {
		t.Fatal("ResolveMounts ignored mountPolicy.denyPaths, want an error")
	}
}

func TestResolveMounts_GuestPathDefaultsToHostPath(t *testing.T) {
	home := withHome(t)
	src := filepath.Join(home, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, err := ResolveMounts([]Mount{{HostPath: src}}, "demo", MountPolicy{})
	if err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}
	want, _ := filepath.EvalSymlinks(src)
	if got[0].GuestPath != filepath.ToSlash(want) {
		t.Errorf("GuestPath = %q, want it defaulted to the host path %q", got[0].GuestPath, want)
	}
}

func TestResolveMounts_RejectsSystemGuestPaths(t *testing.T) {
	home := withHome(t)
	src := filepath.Join(home, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, guestPath := range []string{"/", "/etc", "/usr", "/root"} {
		if _, err := ResolveMounts([]Mount{{HostPath: src, GuestPath: guestPath}}, "demo", MountPolicy{}); err == nil {
			t.Errorf("ResolveMounts accepted guestPath %q, want it rejected", guestPath)
		}
	}
}

func TestResolveMounts_Overlap(t *testing.T) {
	home := withHome(t)
	a := filepath.Join(home, "a")
	b := filepath.Join(home, "b")
	nested := filepath.Join(a, "nested")
	for _, dir := range []string{a, b, nested} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	tests := []struct {
		name   string
		mounts []Mount
	}{
		{"same guest path", []Mount{
			{HostPath: a, GuestPath: "/workspace"},
			{HostPath: b, GuestPath: "/workspace"},
		}},
		{"nested guest paths", []Mount{
			{HostPath: a, GuestPath: "/workspace"},
			{HostPath: b, GuestPath: "/workspace/inner"},
		}},
		{"nested host paths", []Mount{
			{HostPath: a, GuestPath: "/one"},
			{HostPath: nested, GuestPath: "/two"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveMounts(tc.mounts, "demo", MountPolicy{}); err == nil {
				t.Errorf("ResolveMounts accepted overlapping mounts, want an error")
			}
		})
	}
}

// Ordering has to be independent of how profiles and flags happened to
// contribute mounts, because the Incus backend derives device names from
// the resolved set.
func TestResolveMounts_SortedByGuestPath(t *testing.T) {
	home := withHome(t)
	for _, rel := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(home, rel), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	got, err := ResolveMounts([]Mount{
		{HostPath: filepath.Join(home, "c"), GuestPath: "/z"},
		{HostPath: filepath.Join(home, "a"), GuestPath: "/a"},
		{HostPath: filepath.Join(home, "b"), GuestPath: "/m"},
	}, "demo", MountPolicy{})
	if err != nil {
		t.Fatalf("ResolveMounts: %v", err)
	}
	for i, want := range []string{"/a", "/m", "/z"} {
		if got[i].GuestPath != want {
			t.Errorf("mount[%d].GuestPath = %q, want %q", i, got[i].GuestPath, want)
		}
	}
}

// Errors aggregate the way Validate's do, so a profile author sees every
// bad mount at once rather than one per run.
func TestResolveMounts_AggregatesErrors(t *testing.T) {
	home := withHome(t)
	_, err := ResolveMounts([]Mount{
		{HostPath: filepath.Join(home, "missing-one"), GuestPath: "/one"},
		{HostPath: filepath.Join(home, "missing-two"), GuestPath: "/two"},
	}, "demo", MountPolicy{})
	if err == nil {
		t.Fatal("ResolveMounts = nil error, want errors for both mounts")
	}
	for _, want := range []string{"missing-one", "missing-two"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to mention %q", err, want)
		}
	}
}

func TestResolveMounts_RejectsBadTemplate(t *testing.T) {
	withHome(t)
	if _, err := ResolveMounts([]Mount{{HostPath: "~/{{.Nmae}}", GuestPath: "/workspace"}}, "demo", MountPolicy{}); err == nil {
		t.Error("ResolveMounts accepted an unknown template field, want an error")
	}
}

func TestResolveMounts_RejectsFilesystemRoot(t *testing.T) {
	withHome(t)
	if _, err := ResolveMounts([]Mount{{HostPath: "/", GuestPath: "/host"}}, "demo", MountPolicy{}); err == nil {
		t.Error("ResolveMounts accepted / as a host path, want an error")
	}
}

func TestResolveMounts_RejectsNonDirectory(t *testing.T) {
	home := withHome(t)
	file := filepath.Join(home, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := ResolveMounts([]Mount{{HostPath: file, GuestPath: "/workspace"}}, "demo", MountPolicy{}); err == nil {
		t.Error("ResolveMounts accepted a regular file as a mount source, want an error")
	}
}
