package lima

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testHome points the home directory at a temp dir and returns it, so
// "~" expansion is deterministic.
func testHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return home
}

// instanceDir writes limaYAML as the lima.yaml of a fresh instance
// directory under a fresh $LIMA_HOME, and returns the directory.
func instanceDir(t *testing.T, limaYAML string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lima.yaml"), []byte(limaYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func effective(location, mountPoint string, writable bool) limaMountJSON {
	return limaMountJSON{Location: location, MountPoint: &mountPoint, Writable: &writable}
}

func TestCheckMounts(t *testing.T) {
	home := testHome(t)
	const alpha = "- location: /src/alpha\n  mountPoint: /workspace\n  writable: true\n"
	const data = "- location: /data\n  mountPoint: /data\n  writable: false\n"

	cases := []struct {
		name      string
		own       string // the instance's lima.yaml
		effective []limaMountJSON
		wantErr   string // substring; "" means no error
	}{
		{name: "nothing mounted", own: "mounts: []\n"},
		{
			name: "exactly the instance's own mounts",
			// The way Lima writes it: comments, and keys besides mounts.
			own: "# managed by agentctl\ncpus: 2\nmounts:\n" + alpha + data + "user:\n  name: agent\n",
			effective: []limaMountJSON{
				effective("/src/alpha", "/workspace", true),
				effective("/data", "/data", false),
			},
		},
		{name: "an own mount Lima doesn't report", own: "mounts:\n" + alpha},
		{
			name:      "default.yaml adds the home directory",
			own:       "mounts:\n" + alpha,
			effective: []limaMountJSON{effective("/src/alpha", "/workspace", true), effective(home, home, false)},
			wantErr:   "would mount " + home + " at " + home + " (read-only)",
		},
		{
			name:      "default.yaml swaps the directory behind an own mount point",
			own:       "mounts:\n" + alpha,
			effective: []limaMountJSON{effective(home, "/workspace", true)},
			wantErr:   "would mount " + home + " at /workspace (writable)",
		},
		{
			name:      "override.yaml makes a read-only mount writable",
			own:       "mounts:\n" + data,
			effective: []limaMountJSON{effective("/data", "/data", true)},
			wantErr:   "would mount /data at /data (writable)",
		},
		{
			name:      "override.yaml makes a writable mount read-only",
			own:       "mounts:\n" + alpha,
			effective: []limaMountJSON{effective("/src/alpha", "/workspace", false)},
		},
		{
			name:      "an own directory at another mount point",
			own:       "mounts:\n" + data,
			effective: []limaMountJSON{effective("/data", "/elsewhere", false)},
			wantErr:   "at /elsewhere",
		},
		{
			// An instance created before create reset the template's
			// mounts still declares this itself; `start --mount-none`
			// (or --mount) replaces it.
			name:      "the stock templates' home-directory mount, in the instance's own lima.yaml",
			own:       "mounts:\n- location: \"~\"\n",
			effective: []limaMountJSON{effective(home, home, false)},
		},
		{
			name:      "Lima's defaults for a mount that leaves out mountPoint and writable",
			own:       "mounts:\n- location: /data\n",
			effective: []limaMountJSON{{Location: "/data"}},
		},
		{
			name:      "a path template agentctl doesn't expand fails closed",
			own:       "mounts:\n- location: \"{{.Home}}/data\"\n",
			effective: []limaMountJSON{effective(filepath.Join(home, "data"), filepath.Join(home, "data"), false)},
			wantErr:   "beyond what the instance's own configuration mounts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := limaInstanceJSON{Name: "demo", Dir: instanceDir(t, tc.own), Config: &limaConfigJSON{Mounts: tc.effective}}
			err := checkMounts(j)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("checkMounts() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("checkMounts() = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCheckMounts_ErrorSaysWhereTheMountComesFrom(t *testing.T) {
	dir := instanceDir(t, "mounts: []\n")
	j := limaInstanceJSON{Name: "demo", Dir: dir, Config: &limaConfigJSON{
		Mounts: []limaMountJSON{effective("/x", "/x", false)},
	}}
	err := checkMounts(j)
	if err == nil {
		t.Fatal("checkMounts() = nil, want an error")
	}
	limaHome := filepath.Dir(dir)
	for _, want := range []string{
		filepath.Join(limaHome, "_config", "default.yaml"),
		filepath.Join(limaHome, "_config", "override.yaml"),
		"agentctl start demo --mount",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checkMounts() = %q, want it to mention %s", err, want)
		}
	}
}

// TestCheckMounts_CannotVerify: whenever agentctl can't see what Lima
// would mount, or what the instance itself declares, it can't vouch for
// the start, so it refuses it.
func TestCheckMounts_CannotVerify(t *testing.T) {
	mounted := &limaConfigJSON{Mounts: []limaMountJSON{effective("/x", "/x", false)}}
	for name, j := range map[string]limaInstanceJSON{
		"no effective configuration": {Name: "demo"},
		"no instance directory":      {Name: "demo", Config: mounted},
		"no lima.yaml":               {Name: "demo", Dir: t.TempDir(), Config: mounted},
		"malformed lima.yaml":        {Name: "demo", Dir: instanceDir(t, "mounts: [\n"), Config: mounted},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkMounts(j); err == nil {
				t.Error("checkMounts() = nil, want an error")
			}
		})
	}
}
