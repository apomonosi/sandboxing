package profile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/apomonosi/sandboxing/internal/provider"
)

// ResolveMounts turns a profile's declared mounts into the concrete,
// checked host paths a provider can be handed. It is the single place a
// host path is interpreted, and the single enforcement point for "a
// sandbox sees nothing of the host filesystem except what was named."
//
// Every step is deliberately fail-closed: a path that can't be expanded,
// canonicalized or checked is an error, never a pass-through. Errors are
// aggregated with errors.Join (matching Validate) so a profile author sees
// every problem in one pass instead of fixing them one run at a time.
//
// instanceName substitutes into "{{.Name}}"; policy carries the
// admin-authored constraints. The returned slice is sorted by guest path
// so a given mount set always produces the same provider commands
// regardless of the order profiles and flags happened to contribute them —
// which is what lets the Incus backend derive stable device names.
func ResolveMounts(mounts []Mount, instanceName string, policy MountPolicy) ([]provider.Mount, error) {
	return resolveMounts(mounts, instanceName, policy, false)
}

// ResolveMountsPreview is ResolveMounts with every filesystem mutation
// suppressed, for --preview/--dry-run. Without it, previewing a create
// would honor mountPolicy.createMissing and actually create the workspace
// directory — breaking preview's whole contract that nothing happens.
//
// A host path that does not exist is resolved lexically instead of being
// created and canonicalized, so preview shows the path that *would* be
// used. Every check still runs; only the mkdir and the symlink resolution
// that depends on it are skipped.
func ResolveMountsPreview(mounts []Mount, instanceName string, policy MountPolicy) ([]provider.Mount, error) {
	return resolveMounts(mounts, instanceName, policy, true)
}

func resolveMounts(mounts []Mount, instanceName string, policy MountPolicy, dryRun bool) ([]provider.Mount, error) {
	home, homeErr := os.UserHomeDir()

	var (
		errs []error
		out  []provider.Mount
	)
	for i, m := range mounts {
		resolved, err := resolveOne(m, instanceName, home, homeErr, policy, dryRun)
		if err != nil {
			errs = append(errs, fmt.Errorf("mounts[%d] (%s): %w", i, m.HostPath, err))
			continue
		}
		out = append(out, resolved)
	}

	errs = append(errs, checkOverlap(out)...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool { return out[i].GuestPath < out[j].GuestPath })
	return out, nil
}

func resolveOne(m Mount, instanceName, home string, homeErr error, policy MountPolicy, dryRun bool) (provider.Mount, error) {
	hostPath, err := expandHostPath(m.HostPath, instanceName, home, homeErr)
	if err != nil {
		return provider.Mount{}, err
	}

	// Create before canonicalizing: EvalSymlinks fails on a path that
	// doesn't exist yet, and the built-in profiles' per-instance workspace
	// legitimately doesn't on first use.
	missing := false
	if _, statErr := os.Stat(hostPath); statErr != nil {
		if !os.IsNotExist(statErr) {
			return provider.Mount{}, fmt.Errorf("checking host path %s: %w", hostPath, statErr)
		}
		if !policy.CreateMissing {
			return provider.Mount{}, fmt.Errorf("host path %s does not exist (set mountPolicy.createMissing to create it)", hostPath)
		}
		missing = true
		if !dryRun {
			if err := os.MkdirAll(hostPath, 0o700); err != nil {
				return provider.Mount{}, fmt.Errorf("creating host path %s: %w", hostPath, err)
			}
		}
	}

	// Canonicalize before every check below. Without this, a symlink
	// inside an allowed root ("~/src/link" -> "/") satisfies containment
	// while exposing something else entirely.
	//
	// A path that doesn't exist yet under dryRun has nothing to
	// canonicalize, so it stays lexical. That is safe here specifically
	// because it cannot be a symlink: it doesn't exist.
	canonical := hostPath
	if !missing || !dryRun {
		canonical, err = filepath.EvalSymlinks(hostPath)
		if err != nil {
			return provider.Mount{}, fmt.Errorf("resolving host path %s: %w", hostPath, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return provider.Mount{}, fmt.Errorf("checking host path %s: %w", canonical, err)
		}
		if !info.IsDir() {
			return provider.Mount{}, fmt.Errorf("host path %s is not a directory", canonical)
		}
	}

	if err := checkDenied(canonical, home, homeErr, policy); err != nil {
		return provider.Mount{}, err
	}
	if err := checkAllowedRoots(canonical, instanceName, home, homeErr, policy); err != nil {
		return provider.Mount{}, err
	}

	guestPath, err := resolveGuestPath(m.GuestPath, canonical)
	if err != nil {
		return provider.Mount{}, err
	}

	return provider.Mount{HostPath: canonical, GuestPath: guestPath, Writable: m.Writable}, nil
}

// expandHostPath applies the two documented templating features — a
// leading "~" and a "{{.Name}}" placeholder — then makes the result
// absolute. Both are documented in the profile schema and used by the
// built-in profiles, so this is what makes those profiles mean what they
// say rather than being passed to a backend as literal text.
func expandHostPath(raw, instanceName, home string, homeErr error) (string, error) {
	expanded, err := expandNameTemplate(raw, instanceName)
	if err != nil {
		return "", err
	}
	expanded = strings.TrimSpace(expanded)
	if expanded == "" {
		return "", errors.New("hostPath must not be empty")
	}

	if expanded == "~" || strings.HasPrefix(expanded, "~"+string(os.PathSeparator)) || strings.HasPrefix(expanded, "~/") {
		if homeErr != nil {
			return "", fmt.Errorf("expanding %q: resolving home directory: %w", raw, homeErr)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded[1:], "/"))
	}

	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolving %q to an absolute path: %w", expanded, err)
	}
	return filepath.Clean(abs), nil
}

// expandNameTemplate substitutes {{.Name}}. Parsing every hostPath as a
// template (rather than a plain string replace) means an unknown field
// like {{.Nmae}} is reported as an error instead of silently producing a
// path with an empty segment.
func expandNameTemplate(raw, instanceName string) (string, error) {
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}
	tmpl, err := template.New("hostPath").Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing hostPath template %q: %w", raw, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Name string }{Name: instanceName}); err != nil {
		return "", fmt.Errorf("expanding hostPath template %q: %w", raw, err)
	}
	return buf.String(), nil
}

// alwaysDenied are refused regardless of profile configuration.
//
// The provider state directories are the important entries and the least
// obvious: an agent with write access to ~/.lima or Incus's own state can
// rewrite the *next* sandbox's configuration — mounts, devices, network —
// which escapes the sandbox by reconfiguring the thing that builds it,
// without ever attacking the VM boundary itself. Relative to $HOME;
// absolute entries are matched as-is.
var alwaysDenied = []string{
	".ssh",
	".gnupg",
	".aws",
	".config/gcloud",
	".kube",
	".docker",
	".lima",
	".local/share/incus",
	".config/incus",
	".config/agentctl",
}

func checkDenied(canonical, home string, homeErr error, policy MountPolicy) error {
	if canonical == string(filepath.Separator) && !policy.HomeAllowed() {
		return errors.New("refusing to mount the filesystem root (set mountPolicy.allowHome to override)")
	}
	if homeErr == nil {
		canonicalHome, err := filepath.EvalSymlinks(home)
		if err != nil {
			canonicalHome = filepath.Clean(home)
		}
		if canonical == canonicalHome && !policy.HomeAllowed() {
			return fmt.Errorf("refusing to mount the home directory %s: mount the specific project directory instead, or set mountPolicy.allowHome", canonical)
		}
		for _, rel := range alwaysDenied {
			if within(canonical, filepath.Join(canonicalHome, rel)) {
				return fmt.Errorf("%s is never mountable (credentials or sandbox state live there)", canonical)
			}
		}
	}

	for _, deny := range policy.DenyPaths {
		expanded, err := expandHostPath(deny, "", home, homeErr)
		if err != nil {
			return fmt.Errorf("mountPolicy.denyPaths entry %q: %w", deny, err)
		}
		if within(canonical, expanded) {
			return fmt.Errorf("%s is denied by mountPolicy.denyPaths entry %q", canonical, deny)
		}
	}
	return nil
}

func checkAllowedRoots(canonical, instanceName, home string, homeErr error, policy MountPolicy) error {
	if len(policy.AllowedRoots) == 0 {
		return nil
	}
	for _, root := range policy.AllowedRoots {
		expanded, err := expandHostPath(root, instanceName, home, homeErr)
		if err != nil {
			return fmt.Errorf("mountPolicy.allowedRoots entry %q: %w", root, err)
		}
		// A root may legitimately not exist yet (the workspaces directory
		// before first use), so fall back to the lexical path rather than
		// failing the whole resolve.
		if resolvedRoot, err := filepath.EvalSymlinks(expanded); err == nil {
			expanded = resolvedRoot
		}
		if within(canonical, expanded) {
			return nil
		}
	}
	return fmt.Errorf("%s is outside every mountPolicy.allowedRoots entry (%s)", canonical, strings.Join(policy.AllowedRoots, ", "))
}

// within reports whether p is root or lives underneath it. It compares
// path segments rather than string prefixes, so "/home/u/srcevil" is not
// treated as being inside "/home/u/src".
func within(p, root string) bool {
	p = filepath.Clean(p)
	root = filepath.Clean(root)
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// guestPathDenied are guest mount points that would shadow the system the
// guest needs to keep working, or the provisioned user's own home.
var guestPathDenied = []string{"/", "/etc", "/usr", "/bin", "/sbin", "/lib", "/boot", "/dev", "/proc", "/sys", "/var", "/root"}

// resolveGuestPath defaults an omitted guest path to the host path, which
// is what Lima does ("mountPoint builtin default: value of location").
// Guest paths are always POSIX, on every backend, so they use path rather
// than filepath — otherwise this would produce backslashes when agentctl
// runs on Windows.
func resolveGuestPath(raw, canonicalHost string) (string, error) {
	guestPath := strings.TrimSpace(raw)
	if guestPath == "" {
		guestPath = filepath.ToSlash(canonicalHost)
	}
	if !path.IsAbs(guestPath) {
		return "", fmt.Errorf("guestPath %q must be absolute", guestPath)
	}
	guestPath = path.Clean(guestPath)
	for _, denied := range guestPathDenied {
		if guestPath == denied {
			return "", fmt.Errorf("guestPath %q would shadow a system directory inside the guest", guestPath)
		}
	}
	return guestPath, nil
}

// checkOverlap rejects mount sets that can't be expressed coherently: two
// mounts at the same guest path, or one nested inside another. Lima's own
// --mount documentation warns against overlapping mounts; catching it here
// gives a better error than whichever backend notices first, and on Incus
// nested guest paths silently mask one another instead of failing at all.
func checkOverlap(mounts []provider.Mount) []error {
	var errs []error
	for i := 0; i < len(mounts); i++ {
		for j := i + 1; j < len(mounts); j++ {
			a, b := mounts[i], mounts[j]
			switch {
			case a.GuestPath == b.GuestPath:
				errs = append(errs, fmt.Errorf("mounts %s and %s both mount at guestPath %s", a.HostPath, b.HostPath, a.GuestPath))
			case withinPosix(a.GuestPath, b.GuestPath), withinPosix(b.GuestPath, a.GuestPath):
				errs = append(errs, fmt.Errorf("guestPaths %s and %s overlap", a.GuestPath, b.GuestPath))
			case within(a.HostPath, b.HostPath), within(b.HostPath, a.HostPath):
				errs = append(errs, fmt.Errorf("hostPaths %s and %s overlap", a.HostPath, b.HostPath))
			}
		}
	}
	return errs
}

func withinPosix(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
