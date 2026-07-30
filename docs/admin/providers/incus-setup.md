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
- Network ACL support (native since early Incus 6.x; agentctl uses this for
  the egress allowlist/`--deny-lan` enforcement).

## Known operational note

Incus 6.14 patched a bridge-network ACL isolation bypass that could let a
compromised instance intercept traffic from other instances — exactly the
attack class this project defends against. Keep Incus itself patched and
current; `agentctl`'s guarantees are only as good as the backend enforcing
them.

## Verify

```console
$ agentctl config set provider=incus
$ agentctl create test-instance --image=images:alpine/edge
$ agentctl start test-instance
$ agentctl exec test-instance -- echo ok
$ agentctl delete test-instance --force
```
