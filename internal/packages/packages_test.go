package packages

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolve_MapsDivergentNames(t *testing.T) {
	cases := []struct {
		mgr  Manager
		tool string
		want string
	}{
		{APT, "openssh-client", "openssh-client"},
		{DNF, "openssh-client", "openssh-clients"},
		{APK, "openssh-client", "openssh-client"},
		{APT, "procps", "procps"},
		{DNF, "procps", "procps-ng"},
		{APT, "shellcheck", "shellcheck"},
		{DNF, "shellcheck", "ShellCheck"},
		{APT, "xorg", "xserver-xorg"},
		{DNF, "xorg", "xorg-x11-server-Xorg"},
		{APK, "xorg", "xorg-server"},
	}
	for _, c := range cases {
		got, err := Resolve(c.mgr, []string{c.tool})
		if err != nil {
			t.Errorf("Resolve(%s, %q): %v", c.mgr, c.tool, err)
			continue
		}
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("Resolve(%s, %q) = %v, want [%s]", c.mgr, c.tool, got, c.want)
		}
	}
}

// An unknown name is passed through rather than rejected: a user who
// knows their guest can name its package directly. The trade is that such
// a profile stops working on other distributions, which is exactly why
// Catalog exists.
func TestResolve_UnknownNamePassesThrough(t *testing.T) {
	got, err := Resolve(DNF, []string{"ShellCheck", "some-fedora-only-thing"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"ShellCheck", "some-fedora-only-thing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolve_DedupesCollidingNames(t *testing.T) {
	// Both the neutral name and its already-resolved form ask for the
	// same apt package; installing it twice is harmless but noisy.
	got, err := Resolve(APT, []string{"procps", "procps", "git"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"procps", "git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolve_UnsupportedManager(t *testing.T) {
	for _, mgr := range []Manager{"", "pacman", "zypper"} {
		_, err := Resolve(mgr, []string{"git"})
		var unsupported ErrUnsupportedManager
		if !errors.As(err, &unsupported) {
			t.Errorf("Resolve(%q): error = %v, want ErrUnsupportedManager", mgr, err)
			continue
		}
		// The message has to name what does work, or the user is left
		// guessing which images to try.
		for _, want := range []string{"apt", "dnf", "apk"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Resolve(%q) error %q should name the supported manager %q", mgr, err, want)
			}
		}
	}
}

// Every row must cover all three managers. A row with a gap resolves to a
// hard error at create time on that distro, which is worse than not
// listing the tool at all — the profile looks portable and isn't.
func TestCatalog_EveryRowCoversEveryManager(t *testing.T) {
	for tool, row := range Catalog {
		for _, mgr := range Managers {
			if row[mgr] == "" {
				t.Errorf("catalog entry %q has no package name for %s", tool, mgr)
			}
		}
	}
}

func TestCatalog_NoEmptyNames(t *testing.T) {
	for tool, row := range Catalog {
		if err := ValidateName(tool); err != nil {
			t.Errorf("catalog key %q: %v", tool, err)
		}
		for mgr, name := range row {
			if err := ValidateName(name); err != nil {
				t.Errorf("catalog entry %q[%s] = %q: %v", tool, mgr, name, err)
			}
		}
	}
}

func TestKnown_SortedAndComplete(t *testing.T) {
	got := Known()
	if len(got) != len(Catalog) {
		t.Fatalf("Known() returned %d names, catalog has %d", len(got), len(Catalog))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Known() not sorted at %d: %q >= %q", i, got[i-1], got[i])
		}
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"git", "procps-ng", "ShellCheck", "xorg-x11-server-Xorg", "g++", "python3.12", "lib_foo"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	// The shape that matters: a name arriving from a profile can't be
	// anything that would be meaningful to a shell, and a YAML list that
	// collapsed into one string is caught by the space check.
	invalid := []string{"", "-leading-dash", "git; rm -rf /", "git curl", "$(id)", "a`b`", "x|y", "../etc/passwd"}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want an error", name)
		}
	}
}
