package incus

import (
	"os/exec"
	"strings"
	"testing"
)

// embeddedScripts are the shell assets compiled into the binary. They run
// inside a guest, which means a syntax error in one of them surfaces as a
// confusing mid-create failure on someone else's machine rather than as a
// build break here.
func embeddedScripts() map[string]string {
	return map[string]string{
		"bootstrap-user.sh":   bootstrapUserScript,
		"detect-pkgmgr.sh":    detectPkgMgrScript,
		"install-packages.sh": installPackagesScript,
	}
}

// TestEmbeddedScripts_AreValidPOSIXShell parses each script with `sh -n`.
// Cheap, and it catches the class of mistake that is otherwise invisible
// until a real guest runs it — an unbalanced quote, a missing `fi`, a
// function used before it is defined in file order.
//
// Skipped rather than failed when no /bin/sh is available, so the suite
// stays runnable on a machine without one.
func TestEmbeddedScripts_AreValidPOSIXShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH; cannot syntax-check embedded scripts")
	}
	for name, script := range embeddedScripts() {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(sh, "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("sh -n rejected %s: %v\n%s", name, err, out)
			}
		})
	}
}

// Every embedded script is piped over stdin to `sh -s`, so a bash-only
// shebang would be a lie: the interpreter is whatever the guest's /bin/sh
// is, which on Alpine and Debian is not bash.
func TestEmbeddedScripts_TargetPlainSh(t *testing.T) {
	for name, script := range embeddedScripts() {
		first, _, _ := strings.Cut(script, "\n")
		if first != "#!/bin/sh" {
			t.Errorf("%s starts with %q, want \"#!/bin/sh\" — these run under the guest's /bin/sh, not bash", name, first)
		}
	}
}
