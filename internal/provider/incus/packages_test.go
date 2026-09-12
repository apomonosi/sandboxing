package incus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apomonosi/sandboxing/internal/packages"
	"github.com/apomonosi/sandboxing/internal/provider"
)

// callIndex returns the position of the first recorded call whose args
// contain every one of want, or -1. Used to assert *ordering* between
// stages of Create, which is the part of this change that carries real
// consequences.
func callIndex(calls [][]string, want ...string) int {
	for i, call := range calls {
		joined := strings.Join(call, " ")
		matched := true
		for _, w := range want {
			if !strings.Contains(joined, w) {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func createWithPackages(t *testing.T, fr *fakeRunner, pkgs []string) {
	t.Helper()
	p := NewWithRunner(fr).(*Provider)
	spec := provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Packages:  pkgs,
		Overrides: provider.NetworkPolicy{DenyLAN: true},
	}
	if _, err := p.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreate_InstallsResolvedPackageNames(t *testing.T) {
	fr := &fakeRunner{pkgMgr: "dnf"}
	createWithPackages(t, fr, []string{"git", "openssh-client"})

	// dnf's name for the neutral "openssh-client" is "openssh-clients".
	// Asserting on the resolved name is the point: an unresolved one
	// would fail only on a real Fedora guest.
	i := callIndex(fr.calls, "sh -s -- dnf")
	if i < 0 {
		t.Fatalf("no package install call recorded; calls: %v", fr.calls)
	}
	got := strings.Join(fr.calls[i], " ")
	if !strings.Contains(got, "openssh-clients") {
		t.Errorf("install call %q should carry dnf's resolved name \"openssh-clients\"", got)
	}
	if strings.Contains(got, "openssh-client ") {
		t.Errorf("install call %q leaked the neutral name instead of the resolved one", got)
	}
}

// The ordering this change turns on. Packages must be installed while the
// guest still has the backend's default connectivity, because distro
// mirrors are CDN-backed and agentctl's allow rules pin host-resolved
// addresses — so the ACL goes on afterwards.
func TestCreate_WithPackages_AttachesACLAfterProvisioning(t *testing.T) {
	fr := &fakeRunner{}
	createWithPackages(t, fr, []string{"git"})

	install := callIndex(fr.calls, "sh -s -- apt")
	acl := callIndex(fr.calls, "network acl create")
	if install < 0 {
		t.Fatalf("no package install call recorded; calls: %v", fr.calls)
	}
	if acl < 0 {
		t.Fatalf("no ACL creation recorded; calls: %v", fr.calls)
	}
	if acl < install {
		t.Errorf("ACL was created at call %d, before the package install at %d — "+
			"the install would have run against a locked-down network", acl, install)
	}
}

// ...and with no packages requested, nothing about the existing order
// changes: the ACL is attached before the instance is ever started.
func TestCreate_WithoutPackages_AttachesACLBeforeStart(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)
	spec := provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Overrides: provider.NetworkPolicy{DenyLAN: true},
	}
	if _, err := p.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	acl := callIndex(fr.calls, "network acl create")
	start := callIndex(fr.calls, "start demo")
	if acl < 0 || start < 0 {
		t.Fatalf("expected both an ACL creation and a start; calls: %v", fr.calls)
	}
	if acl > start {
		t.Errorf("ACL created at call %d, after start at %d; a plain create must not widen the window", acl, start)
	}
	if i := callIndex(fr.calls, "AGENTCTL_PKGMGR"); i >= 0 {
		t.Error("a create with no packages must not probe the guest's package manager")
	}
	for _, call := range fr.calls {
		if strings.Contains(strings.Join(call, " "), "sh -s -- apt") {
			t.Errorf("a create with no packages ran an install: %v", call)
		}
	}
}

// --no-network-policy already means "no ACL at all". Requesting packages
// alongside it must not resurrect one at the end.
func TestCreate_UnrestrictedWithPackages_StillAppliesNoACL(t *testing.T) {
	fr := &fakeRunner{}
	p := NewWithRunner(fr).(*Provider)
	spec := provider.InstanceSpec{
		Name:      "demo",
		Image:     "images:ubuntu/24.04",
		Packages:  []string{"git"},
		Overrides: provider.NetworkPolicy{DenyLAN: true, Unrestricted: true},
	}
	if _, err := p.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if i := callIndex(fr.calls, "network acl"); i >= 0 {
		t.Errorf("--no-network-policy created an ACL at call %d: %v", i, fr.calls[i])
	}
}

// Both shapes of "agentctl can't install into this guest": a manager it
// doesn't drive, and no recognized manager at all. Each must fail with a
// message naming what does work, rather than a bare exit code from an
// install that was never going to succeed.
func TestCreate_UnsupportedGuestPackageManager_Fails(t *testing.T) {
	for _, reported := range []string{"pacman", "none"} {
		t.Run(reported, func(t *testing.T) {
			fr := &fakeRunner{pkgMgr: reported}
			p := NewWithRunner(fr).(*Provider)
			_, err := p.Create(context.Background(), provider.InstanceSpec{
				Name: "demo", Image: "images:arch", Packages: []string{"git"},
			})
			if err == nil {
				t.Fatal("expected Create to fail on a guest agentctl can't install into")
			}
			var unsupported packages.ErrUnsupportedManager
			if !errors.As(err, &unsupported) {
				t.Errorf("error = %v, want it to wrap ErrUnsupportedManager", err)
			}
			if !strings.Contains(err.Error(), "apt") {
				t.Errorf("error %q should name the distributions that do work", err)
			}
		})
	}
}

func TestCreate_PackageInstallFailure_SurfacesStderr(t *testing.T) {
	fr := &fakeRunner{installFailCode: 1}
	p := NewWithRunner(fr).(*Provider)
	_, err := p.Create(context.Background(), provider.InstanceSpec{
		Name: "demo", Image: "images:ubuntu/24.04", Packages: []string{"git"},
	})
	if err == nil {
		t.Fatal("expected Create to fail when the package install exits non-zero")
	}
	// The package manager's own message is the only thing that tells the
	// user which package was the problem.
	if !strings.Contains(err.Error(), "no-such-package") {
		t.Errorf("error %q should carry the package manager's stderr", err)
	}
}

// Preview must describe the create that will actually run — including the
// reordering, which is the whole point of the flag.
func TestPreviewCreate_WithPackages_MirrorsDeferredACL(t *testing.T) {
	p := NewWithRunner(&fakeRunner{}).(*Provider)
	cmds := p.PreviewCreate(provider.InstanceSpec{
		Name: "demo", Image: "images:ubuntu/24.04",
		Packages:  []string{"git"},
		Overrides: provider.NetworkPolicy{DenyLAN: true},
	})

	var flat [][]string
	for _, c := range cmds {
		flat = append(flat, c.Args)
	}
	acl := callIndex(flat, "network acl create")
	stop := callIndex(flat, "stop demo")
	if acl < 0 || stop < 0 {
		t.Fatalf("preview missing ACL or stop: %v", flat)
	}
	if acl < stop {
		t.Errorf("preview shows the ACL at %d, before the stop at %d — Create defers it past provisioning", acl, stop)
	}
	if callIndex(flat, "sh -s -- <detected>", "git") < 0 {
		t.Errorf("preview should show the package install; got %v", flat)
	}
}
