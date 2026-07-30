# Installation

## 1. Install the `agentctl` binary

This milestone doesn't yet ship prebuilt binaries or platform packages (see the
project's out-of-scope list) — build from source for now:

```console
$ go install github.com/apomonosi/sandboxing/cmd/agentctl@latest
```

This works identically on Linux, macOS, and Windows — `agentctl` itself has no
runtime dependency beyond the Go toolchain used to build it.

## 2. Install the backend for your OS

`agentctl` doesn't provide isolation itself; it drives an OS-native backend.
Install and set up the one for your platform before continuing:

- Linux: [Incus setup](../admin/providers/incus-setup.md)
- macOS: [Lima setup](../admin/providers/lima-setup.md)
- Windows: [Hyper-V setup](../admin/providers/hyperv-setup.md)

Only the Linux/Incus backend is fully implemented this milestone — the macOS
and Windows pages describe what's coming and what to expect from `agentctl` in
the meantime (see the [capability matrix](../admin/capability-matrix.md)).

## 3. Tell agentctl which backend to use

```console
$ agentctl config set provider=incus   # or lima, or hyperv
```

This is a one-time, per-user choice, stored in
`~/.config/agentctl/config.yaml` (override the path with `$AGENTCTL_CONFIG`).
If your organization distributes a shared profile directory, set that too:

```console
$ agentctl config set profileDir=/path/to/shared/profiles
```

See [Deployment Model](../admin/deployment-model.md) if you're rolling this out
to a team rather than just yourself.

## 4. Verify

```console
$ agentctl profile list
default
strict
```

If that prints without error, you're ready for the [Quickstart](quickstart.md).
