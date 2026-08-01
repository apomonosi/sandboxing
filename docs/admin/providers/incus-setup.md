# Incus Setup (Linux)

This is the fully implemented backend this milestone.

## Install

Follow the [official Incus install instructions](https://linuxcontainers.org/incus/docs/main/installing/)
for your distribution — current stable is the 7.0 LTS line (formerly the
6.23 series). `agentctl` shells out to the `incus` CLI directly, so any
install method that puts a working `incus` binary on `$PATH` and an
initialized daemon in place is sufficient.

## Initialize

```console
$ sudo incus admin init --minimal
```

`--minimal` is enough to get started; see Incus's own docs for storage/network
backend choices appropriate to your environment.

## Permissions

Add your user to the `incus-admin` group (or whichever group your
distribution uses) so `incus` commands don't require `sudo`:

```console
$ sudo usermod -aG incus-admin $USER
```

Log out and back in for the group change to take effect. `agentctl` assumes
this is already set up — it does not manage Incus permissions itself.

## What agentctl assumes exists

- A running, initialized Incus daemon reachable via the local `incus` CLI.
- Enough storage/network configuration for `incus init --vm` to succeed
  (agentctl creates VMs, not containers, for the stronger isolation boundary).
- **Working hardware virtualization (KVM).** Since agentctl always creates
  VMs, the host needs `/dev/kvm` available — a real machine or a VM host
  with nested virtualization enabled. This is why
  `.github/workflows/ci-integration.yml` can't run on a standard
  GitHub-hosted Actions runner: those have no nested virtualization, so
  Incus fails immediately with "Instance type \"virtual-machine\" is not
  supported on this server: QEMU command not available for CPU
  architecture." That workflow needs to be pointed at a self-hosted or
  nested-virt-capable runner.
- Network ACL support (native since early Incus 6.x; agentctl uses this for
  the egress allowlist/`--deny-lan` enforcement).

## Known operational notes

- Incus 6.14 patched a bridge-network ACL isolation bypass that could let a
  compromised instance intercept traffic from other instances — exactly the
  attack class this project defends against. Keep Incus itself patched and
  current; `agentctl`'s guarantees are only as good as the backend enforcing
  them.
- `agentctl create` always passes `-c security.secureboot=false`. Incus VMs
  default to requiring UEFI Secure Boot, but most public/community images
  (Alpine, generic Ubuntu cloud images, ...) aren't Secure Boot signed and
  simply refuse to start otherwise. This doesn't weaken agentctl's actual
  security guarantees — Secure Boot protects a guest's own boot chain
  against tampering with its boot media, which is orthogonal to the
  host-escape/LAN-lateral-movement threat model this project defends
  against. If you're using an image that *is* Secure Boot signed and want
  it enforced, re-enable it by hand after creation:
  `incus config set <name> security.secureboot=true`.
- `agentctl create` also provisions a non-root default user (see
  [Profiles & Policies](../../user/profiles-and-policies.md#default-non-root-user))
  by running a small bootstrap script inside the instance right after network
  policy is applied. It detects `useradd` (Debian/Ubuntu family) or `adduser`
  (Alpine/busybox family) — images using neither will fail this step with a
  clear error rather than silently staying root-only. It attempts a fixed
  UID (1500) for predictability and falls back to whatever the OS assigns if
  that's already taken; the actual result is recorded on the instance via
  `incus config get <name> user.agentctl-shell-user` (colon-delimited
  `username:uid:home`), which `shell`/`exec` read back on every invocation —
  agentctl keeps no state of its own here, Incus's own per-instance config is
  the source of truth. Re-running the bootstrap step (e.g. on a retry) is a
  no-op if the user already exists.

## Verify

```console
$ agentctl config set provider=incus
$ agentctl create test-instance --image=images:alpine/edge
$ agentctl start test-instance
$ agentctl exec test-instance -- whoami        # -> agent (non-root default)
$ agentctl exec --root test-instance -- whoami # -> root
$ agentctl delete test-instance --force
```
