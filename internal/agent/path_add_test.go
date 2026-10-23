package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// piProfileLine is what agentctl_path_add wrote for pi before it moved
// into path-add.sh; a guest that already has it must not get a second,
// differently spelled one.
const piProfileLine = `export PATH="$HOME/.pi/agent/bin:$PATH"`

// pathStep is one run of agentctl_agent_on_path.
type pathStep struct {
	out     string // combined output, then a final "PATH=..." line
	profile string // ~/.profile afterwards
}

// runOnPath runs agentctl_agent_on_path AGENT BINARY under every shell
// available, with HOME set to home and the host's PATH plus extraPath, and
// with path-add.sh's /usr/local/bin replaced by sysDir so that candidate
// can be tested without writing there. It fails the test if the step
// exits non-zero, which it must never do.
func runOnPath(t *testing.T, home, sysDir, extraPath, agent, binary string) pathStep {
	t.Helper()
	script := strings.ReplaceAll(pathAdd, "/usr/local/bin", sysDir)
	var res pathStep
	for _, sh := range shells(t) {
		cmd := exec.Command(sh, "-c", script+"agentctl_agent_on_path "+agent+" "+binary+"\necho \"PATH=$PATH\"")
		path := os.Getenv("PATH")
		if extraPath != "" {
			path = extraPath + ":" + path
		}
		cmd.Env = []string{"PATH=" + path, "HOME=" + home}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: agentctl_agent_on_path failed, but it must never fail the install: %v\n%s", sh, err, out)
		}
		res.out = string(out)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".profile"))
	res.profile = string(data)
	return res
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Each candidate directory in turn: the one holding the binary goes first
// on PATH for the rest of the script and into ~/.profile, once, however
// many times the step runs (every shell in shells, here).
func TestAgentOnPath_CandidateDirectories(t *testing.T) {
	cases := []struct {
		rel  string // under HOME, or "" for the system directory
		line string
	}{
		{".local/bin", `export PATH="$HOME/.local/bin:$PATH"`},
		{".fakeagent/bin", `export PATH="$HOME/.fakeagent/bin:$PATH"`},
		{".fakeagent/agent/bin", `export PATH="$HOME/.fakeagent/agent/bin:$PATH"`},
		{"", ""}, // filled in below: depends on the temporary directory
	}
	for _, c := range cases {
		home, sys := t.TempDir(), t.TempDir()
		dir := sys
		if c.rel != "" {
			dir = filepath.Join(home, c.rel)
		} else {
			c.line = `export PATH="` + sys + `:$PATH"`
		}
		writeExecutable(t, filepath.Join(dir, "fakeagent-cli"))
		if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("# skel\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		res := runOnPath(t, home, sys, "", "fakeagent", "fakeagent-cli")
		if !strings.Contains(res.out, "PATH="+dir+":") {
			t.Errorf("%s: rest of the script does not get %s first on PATH:\n%s", c.rel, dir, res.out)
		}
		if strings.Contains(res.out, "agentctl:") {
			t.Errorf("%s: binary present, but got a notice:\n%s", c.rel, res.out)
		}
		if want := "# skel\n" + c.line + "\n"; res.profile != want {
			t.Errorf("%s: ~/.profile = %q, want %q", c.rel, res.profile, want)
		}
		if res = runOnPath(t, home, sys, "", "fakeagent", "fakeagent-cli"); strings.Count(res.profile, c.line) != 1 {
			t.Errorf("%s: after repeated runs ~/.profile has the PATH line %d times, want 1:\n%s", c.rel, strings.Count(res.profile, c.line), res.profile)
		}
	}
}

// An earlier candidate wins over a later one.
func TestAgentOnPath_FirstCandidateWins(t *testing.T) {
	home, sys := t.TempDir(), t.TempDir()
	writeExecutable(t, filepath.Join(home, ".fakeagent", "bin", "fakeagent"))
	writeExecutable(t, filepath.Join(sys, "fakeagent"))
	res := runOnPath(t, home, sys, "", "fakeagent", "fakeagent")
	if want := `export PATH="$HOME/.fakeagent/bin:$PATH"` + "\n"; res.profile != want {
		t.Errorf("~/.profile = %q, want %q", res.profile, want)
	}
}

// A binary already on PATH needs nothing: no PATH change, no ~/.profile.
func TestAgentOnPath_AlreadyOnPath(t *testing.T) {
	home, sys, onPath := t.TempDir(), t.TempDir(), t.TempDir()
	writeExecutable(t, filepath.Join(onPath, "fakeagent"))
	writeExecutable(t, filepath.Join(home, ".local", "bin", "fakeagent"))
	res := runOnPath(t, home, sys, onPath, "fakeagent", "fakeagent")
	if res.profile != "" {
		t.Errorf("binary already on PATH, but ~/.profile = %q", res.profile)
	}
	if !strings.HasSuffix(res.out, "PATH="+onPath+":"+os.Getenv("PATH")+"\n") {
		t.Errorf("binary already on PATH, but PATH changed:\n%s", res.out)
	}
	if strings.Contains(res.out, "agentctl:") {
		t.Errorf("binary already on PATH, but got a notice:\n%s", res.out)
	}
}

// A binary in none of the candidates gets one notice and a zero exit
// (runOnPath fails the test otherwise), and nothing is written.
func TestAgentOnPath_NotFound(t *testing.T) {
	home, sys := t.TempDir(), t.TempDir()
	res := runOnPath(t, home, sys, "", "fakeagent", "fakeagent-cli")
	if n := strings.Count(res.out, "agentctl: fakeagent: "); n != 1 {
		t.Errorf("want one agentctl: fakeagent: notice, got %d:\n%s", n, res.out)
	}
	if !strings.Contains(res.out, "no fakeagent-cli on PATH") {
		t.Errorf("notice does not name the binary:\n%s", res.out)
	}
	if res.profile != "" {
		t.Errorf("binary missing, but ~/.profile = %q", res.profile)
	}
}

// pi's ~/.profile line is byte-identical to the one written before the
// PATH step became generic.
func TestAgentOnPath_PiLineUnchanged(t *testing.T) {
	home := t.TempDir()
	writeExecutable(t, filepath.Join(home, ".pi", "agent", "bin", "pi"))
	if res := runOnPath(t, home, t.TempDir(), "", "pi", "pi"); res.profile != piProfileLine+"\n" {
		t.Errorf("~/.profile = %q, want %q", res.profile, piProfileLine+"\n")
	}
}

// Every agent's script ends by putting its binary on PATH, and only once
// its installer has succeeded.
func TestRegistry_EveryAgentPutsItselfOnPath(t *testing.T) {
	for name, spec := range Registry {
		if !strings.HasPrefix(spec.InstallScript, pathAdd) {
			t.Errorf("%s: install script does not start with the PATH helper", name)
		}
		binary := name
		if name == "cursor" {
			binary = "cursor-agent"
		}
		if want := " || exit\nagentctl_agent_on_path " + name + " " + binary; !strings.HasSuffix(spec.InstallScript, want) {
			t.Errorf("%s: install script should end with %q", name, want)
		}
	}
}

// fakeInstallerCurl stands in for curl: it writes to the -o file an
// installer that drops $FAKE_BIN into $HOME/.local/bin, as claude's does,
// and exits $FAKE_STATUS.
const fakeInstallerCurl = `#!/bin/sh
while [ $# -gt 0 ]; do
	[ "$1" = -o ] && out=$2
	shift
done
cat >"$out" <<'INSTALLER'
mkdir -p "$HOME/.local/bin"
printf '#!/bin/sh\n' >"$HOME/.local/bin/$FAKE_BIN"
chmod +x "$HOME/.local/bin/$FAKE_BIN"
exit "$FAKE_STATUS"
INSTALLER
`

// Each curl agent's whole script, run with only the tools it needs on
// PATH (so a real agent binary on this host is out of reach): a
// successful installer ends with the agent on PATH via ~/.profile, and a
// failed one fails the script with its status and writes nothing.
func TestRegistry_InstallScriptPutsAgentOnPath(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	bin := t.TempDir()
	for _, tool := range []string{"sh", "bash", "grep", "mkdir", "chmod", "mktemp", "rm", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("no %s on PATH", tool)
		}
		if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(fakeInstallerCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex", "opencode", "cursor"} {
		binary := name
		if name == "cursor" {
			binary = "cursor-agent"
		}
		for _, status := range []string{"0", "3"} {
			home := t.TempDir()
			cmd := exec.Command(sh, "-c", Registry[name].InstallScript+"\ncommand -v "+binary)
			cmd.Env = []string{"PATH=" + bin, "HOME=" + home, "TMPDIR=" + t.TempDir(), "FAKE_BIN=" + binary, "FAKE_STATUS=" + status}
			out, err := cmd.CombinedOutput()
			profile, _ := os.ReadFile(filepath.Join(home, ".profile"))
			if status == "3" {
				if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
					t.Errorf("%s: failed installer should fail the script with 3, got %v\n%s", name, err, out)
				}
				if len(profile) != 0 {
					t.Errorf("%s: failed installer, but ~/.profile = %q", name, profile)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: install script failed: %v\n%s", name, err, out)
			}
			if want := filepath.Join(home, ".local", "bin", binary); !strings.Contains(string(out), want) {
				t.Errorf("%s: %s not runnable by name after the script:\n%s", name, binary, out)
			}
			if want := `export PATH="$HOME/.local/bin:$PATH"` + "\n"; string(profile) != want {
				t.Errorf("%s: ~/.profile = %q, want %q", name, profile, want)
			}
		}
	}
}

// Install scripts must also pass shellcheck as POSIX sh, assembled as the
// guest receives them.
func TestRegistry_InstallScriptsPassShellcheck(t *testing.T) {
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("no shellcheck on PATH")
	}
	for name, spec := range Registry {
		cmd := exec.Command(sc, "-s", "sh", "-")
		cmd.Stdin = strings.NewReader(spec.InstallScript)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("shellcheck -s sh rejected %s's install script: %v\n%s", name, err, out)
		}
	}
}
