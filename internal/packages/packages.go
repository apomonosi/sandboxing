// Package packages maps agentctl's neutral tool names onto the package
// names each guest distribution's manager actually uses, so a profile can
// say `packages: [ripgrep, openssh-client]` once and have it install on
// Ubuntu, Fedora and Alpine alike.
//
// Shape mirrors internal/agent and internal/image: a small static table,
// no network lookups, no plugin mechanism.
package packages

import (
	"fmt"
	"sort"
	"strings"
)

// Manager identifies a guest package manager. Deliberately a short, closed
// set — apt, dnf and apk are the three the images used throughout this
// project's docs and tests actually have (images:ubuntu/24.04,
// images:fedora/44, images:alpine/edge).
//
// pacman and zypper are absent on purpose rather than by oversight. Adding
// them means adding a column to every row of Catalog below, and those
// entries could only be transcribed from memory — the exact thing the
// "verified directly from each project's own docs" rule in internal/agent
// exists to prevent. A guest running either reports ErrUnsupportedManager
// and the user installs by hand, which is honest; a table full of guessed
// package names that fail halfway through a create is not.
type Manager string

const (
	APT Manager = "apt"
	DNF Manager = "dnf"
	APK Manager = "apk"
)

// Managers is every supported Manager, for error messages.
var Managers = []Manager{APT, DNF, APK}

// ErrUnsupportedManager is returned by Resolve when the guest's package
// manager isn't one agentctl knows how to drive.
type ErrUnsupportedManager struct {
	Manager Manager
}

func (e ErrUnsupportedManager) Error() string {
	names := make([]string, len(Managers))
	for i, m := range Managers {
		names[i] = string(m)
	}
	if e.Manager == "" {
		return "no supported package manager found in the guest (agentctl drives " +
			strings.Join(names, ", ") + "); install the packages by hand, or use an image based on one of those distributions"
	}
	return fmt.Sprintf("guest package manager %q is not one agentctl drives (%s)", e.Manager, strings.Join(names, ", "))
}

// Catalog maps a neutral tool name to the per-manager package name.
//
// Only entries whose name genuinely differs across managers need to be
// here at all — a name absent from this table passes through verbatim (see
// Resolve), which is why `git`, `curl` and `jq` are listed anyway: being
// able to see at a glance which tools agentctl considers part of its
// vocabulary is worth the three redundant rows.
//
// A missing entry for one manager (rather than a missing row) means the
// tool is not packaged under a name this project has confirmed for that
// distribution; Resolve reports it instead of guessing.
var Catalog = map[string]map[Manager]string{
	// --- Universally identical names -------------------------------------
	"git":             {APT: "git", DNF: "git", APK: "git"},
	"curl":            {APT: "curl", DNF: "curl", APK: "curl"},
	"ca-certificates": {APT: "ca-certificates", DNF: "ca-certificates", APK: "ca-certificates"},
	"jq":              {APT: "jq", DNF: "jq", APK: "jq"},
	"ripgrep":         {APT: "ripgrep", DNF: "ripgrep", APK: "ripgrep"},
	"unzip":           {APT: "unzip", DNF: "unzip", APK: "unzip"},
	"tar":             {APT: "tar", DNF: "tar", APK: "tar"},
	"make":            {APT: "make", DNF: "make", APK: "make"},
	"gcc":             {APT: "gcc", DNF: "gcc", APK: "gcc"},
	"less":            {APT: "less", DNF: "less", APK: "less"},
	"vim":             {APT: "vim", DNF: "vim", APK: "vim"},
	"python3":         {APT: "python3", DNF: "python3", APK: "python3"},
	"nodejs":          {APT: "nodejs", DNF: "nodejs", APK: "nodejs"},
	"npm":             {APT: "npm", DNF: "npm", APK: "npm"},
	"openbox":         {APT: "openbox", DNF: "openbox", APK: "openbox"},
	"xterm":           {APT: "xterm", DNF: "xterm", APK: "xterm"},

	// --- Names that actually differ --------------------------------------
	"openssh-client": {APT: "openssh-client", DNF: "openssh-clients", APK: "openssh-client"},
	"procps":         {APT: "procps", DNF: "procps-ng", APK: "procps"},
	// Fedora's package is capitalized; apt's and apk's are not.
	"shellcheck": {APT: "shellcheck", DNF: "ShellCheck", APK: "shellcheck"},
	"xorg":       {APT: "xserver-xorg", DNF: "xorg-x11-server-Xorg", APK: "xorg-server"},
	"xinit":      {APT: "xinit", DNF: "xorg-x11-xinit", APK: "xinit"},
}

// Resolve turns neutral tool names into the package names mgr installs.
//
// A name that isn't in Catalog passes through unchanged, so a user who
// knows their guest can write a raw distro package name and have it work.
// That is an escape hatch, not the intended path: such a profile stops
// being portable across images, which is the whole reason Catalog exists.
//
// Returns the resolved names in the order given, with duplicates removed
// (two neutral names can map to the same package).
func Resolve(mgr Manager, tools []string) ([]string, error) {
	if !supported(mgr) {
		return nil, ErrUnsupportedManager{Manager: mgr}
	}
	seen := make(map[string]bool, len(tools))
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := tool
		if row, ok := Catalog[tool]; ok {
			mapped, ok := row[mgr]
			if !ok {
				return nil, fmt.Errorf("tool %q has no known package name for %s; install it by hand or name the distro package directly", tool, mgr)
			}
			name = mapped
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

func supported(mgr Manager) bool {
	for _, m := range Managers {
		if m == mgr {
			return true
		}
	}
	return false
}

// Known returns every neutral tool name in Catalog, sorted — for
// documentation and error messages.
func Known() []string {
	names := make([]string, 0, len(Catalog))
	for n := range Catalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ValidateName rejects anything that isn't shaped like a package name.
//
// These names arrive from a profile — an org-distributed YAML artifact —
// and end up as arguments to a package manager running as root inside the
// guest. They are passed as separate argv entries, never interpolated into
// a command string, so this is a second line of defence rather than the
// only one; it also catches the much more likely case of a typo'd YAML
// list that silently became one long string.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("empty package name")
	}
	if !isAlnum(rune(name[0])) {
		return fmt.Errorf("package name %q must start with a letter or digit", name)
	}
	for _, r := range name {
		if isAlnum(r) || strings.ContainsRune("._+-", r) {
			continue
		}
		return fmt.Errorf("package name %q contains %q; expected only letters, digits, and . _ + -", name, r)
	}
	return nil
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
