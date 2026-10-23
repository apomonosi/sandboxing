package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A curl installer piped into a shell records a refused download as a
// successful install, because the pipeline exits with the shell's status
// and a shell given an empty script exits 0. Every one goes through
// agentctl_fetch_run instead.
func TestRegistry_NoInstallerPipedIntoShell(t *testing.T) {
	pipe := regexp.MustCompile(`\|\s*(sh|bash)\b`)
	for name, spec := range Registry {
		if m := pipe.FindString(spec.InstallScript); m != "" {
			t.Errorf("%s: InstallScript pipes into a shell (%q); use agentctl_fetch_run", name, m)
		}
	}
}

// Each curl installer keeps its agent's own shell, URL and curl flags,
// after the helper that defines agentctl_fetch_run, and its failure stops
// the script before the PATH step that follows it.
func TestRegistry_InstallersUseFetchRun(t *testing.T) {
	want := map[string]string{
		"claude":   "agentctl_fetch_run claude bash https://claude.ai/install.sh -fsSL",
		"codex":    "agentctl_fetch_run codex sh https://chatgpt.com/codex/install.sh -fsSL",
		"opencode": "agentctl_fetch_run opencode bash https://opencode.ai/install -fsSL",
		"pi":       "agentctl_fetch_run pi sh https://pi.dev/install.sh -fsSL",
		"cursor":   "agentctl_fetch_run cursor bash https://cursor.com/install -fsS",
	}
	for name, call := range want {
		script := Registry[name].InstallScript
		if !strings.Contains(script, fetchRun) {
			t.Errorf("%s: install script does not include the fetch helper", name)
		}
		if !strings.Contains(script, "\n"+call+" || exit\nagentctl_agent_on_path "+name+" ") {
			t.Errorf("%s: install script should run %q, exit on its failure, then put %s on PATH, got %q", name, call, name, script)
		}
		// pi's script also carries the Node and tools bootstraps between
		// the two helpers.
		if name != "pi" && !strings.HasPrefix(script, pathAdd+fetchRun) {
			t.Errorf("%s: install script does not start with the PATH and fetch helpers", name)
		}
	}
	if got, want := Registry["gemini"].InstallScript, pathAdd+"npm install -g @google/gemini-cli || exit\nagentctl_agent_on_path gemini gemini"; got != want {
		t.Errorf("gemini: install script changed to %q", got[len(pathAdd):])
	}
}

// Install scripts run under the guest's /bin/sh, which on Alpine and
// Debian is not bash; dash is checked as well when present.
func TestRegistry_InstallScriptsAreValidPOSIXShell(t *testing.T) {
	for _, shell := range []string{"sh", "dash"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		for name, spec := range Registry {
			if out, err := exec.Command(path, "-n", "-c", spec.InstallScript).CombinedOutput(); err != nil {
				t.Errorf("%s -n rejected %s's install script: %v\n%s", shell, name, err, out)
			}
		}
	}
}

// fakeCurl stands in for curl: it records its arguments in $CURL_ARGS
// and, per $CURL_MODE, fails the way curl -f does on a 403, or writes a
// script that prints which shell ran it and exits $SCRIPT_STATUS to the
// -o file.
const fakeCurl = `#!/bin/sh
echo "$*" >"$CURL_ARGS"
while [ $# -gt 0 ]; do
	[ "$1" = -o ] && out=$2
	shift
done
if [ "$CURL_MODE" = fail ]; then
	echo "curl: (22) The requested URL returned error: 403" >&2
	exit 22
fi
printf 'echo "ran by $RUN_BY"\nexit %s\n' "$SCRIPT_STATUS" >"$out"
`

type fetchRunResult struct {
	out      string
	status   int
	curlArgs string
	tmpLeft  []string
}

// runFetchRun runs body after the helper under shell, with the fake curl
// first on PATH and TMPDIR pointing at an empty directory, so whatever
// mktemp left behind is what is in it afterwards.
func runFetchRun(t *testing.T, shell, mode, scriptStatus, body string) fetchRunResult {
	t.Helper()
	bin, tmp := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(t.TempDir(), "args")
	cmd := exec.Command(shell, "-c", fetchRun+body)
	cmd.Env = []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"TMPDIR=" + tmp,
		"CURL_MODE=" + mode,
		"CURL_ARGS=" + args,
		"SCRIPT_STATUS=" + scriptStatus,
		"RUN_BY=" + shell,
	}
	out, err := cmd.CombinedOutput()
	res := fetchRunResult{out: string(out)}
	if ee, ok := err.(*exec.ExitError); ok {
		res.status = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s: %v", shell, err)
	}
	if a, err := os.ReadFile(args); err == nil {
		res.curlArgs = strings.TrimSpace(string(a))
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		res.tmpLeft = append(res.tmpLeft, e.Name())
	}
	return res
}

func shells(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, s := range []string{"sh", "dash"} {
		if p, err := exec.LookPath(s); err == nil {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		t.Skip("no sh on PATH")
	}
	return found
}

// A refused download is a failed install with one line naming the agent
// and the URL, and the downloaded file is never run.
func TestFetchRun_DownloadFails(t *testing.T) {
	const want = "agentctl: pi: could not download https://pi.dev/install.sh (curl exit 22); the sandbox's egress policy may be refusing it — check the agent's allow preset"
	for _, sh := range shells(t) {
		r := runFetchRun(t, sh, "fail", "0", "agentctl_fetch_run pi sh https://pi.dev/install.sh -fsSL")
		if r.status == 0 {
			t.Errorf("%s: status 0 after a failed download\n%s", sh, r.out)
		}
		if n := strings.Count(r.out, "agentctl: pi:"); n != 1 {
			t.Errorf("%s: %d agentctl: pi: lines, want 1\n%s", sh, n, r.out)
		}
		if !strings.Contains(r.out, want+"\n") {
			t.Errorf("%s: output %q does not contain %q", sh, r.out, want)
		}
		if strings.Contains(r.out, "ran by") {
			t.Errorf("%s: installer ran after a failed download\n%s", sh, r.out)
		}
		if len(r.tmpLeft) != 0 {
			t.Errorf("%s: temporary files left behind: %v", sh, r.tmpLeft)
		}
	}
}

// A downloaded installer runs under the agent's shell, with the agent's
// curl flags, and its status is the install's.
func TestFetchRun_RunsInstaller(t *testing.T) {
	for _, sh := range shells(t) {
		for _, status := range []int{0, 7} {
			r := runFetchRun(t, sh, "ok", strconv.Itoa(status),
				"agentctl_fetch_run cursor sh https://cursor.com/install -fsS")
			if r.status != status {
				t.Errorf("%s: status %d, want the installer's %d\n%s", sh, r.status, status, r.out)
			}
			if !strings.Contains(r.out, "ran by ") {
				t.Errorf("%s: installer did not run\n%s", sh, r.out)
			}
			if strings.Contains(r.out, "agentctl:") {
				t.Errorf("%s: download succeeded, but got an agentctl: line\n%s", sh, r.out)
			}
			if !strings.HasPrefix(r.curlArgs, "-fsS -o ") || !strings.HasSuffix(r.curlArgs, " https://cursor.com/install") {
				t.Errorf("%s: curl called with %q, want the agent's flags, -o <file>, then the URL", sh, r.curlArgs)
			}
			if len(r.tmpLeft) != 0 {
				t.Errorf("%s: temporary files left behind: %v", sh, r.tmpLeft)
			}
		}
	}
}
