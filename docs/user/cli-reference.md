# CLI Reference

## Exit codes

Every `agentctl` command uses the same exit code convention, so scripts and
CI can branch on it instead of parsing text:

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Generic runtime error |
| `2` | Capability gap: the feature is **not available** or **under development** on the active provider |
| `3` | A **manual workaround** exists but wasn't applied (re-run with `--force-partial` after applying it, or apply it and re-run) |

Pass `--output=json` on any command to get machine-readable output, including
structured capability-gate outcomes (`status`, `feature`, `message`, `plan`,
`tracking_url`) instead of the human-readable text.

## Global flags

| Flag | Purpose |
|---|---|
| `--provider <name>` | Override the configured provider for this invocation |
| `--output <table\|json>` | Output format (default `table`) |
| `--profile-dir <path>` | Override the configured org-distributed profile directory |
| `--force-partial` | Proceed past a `ManualWorkaround` capability gap, accepting reduced protection |
| `--preview` / `--dry-run` | Print the backend command(s) this would run, instead of running them — see [Preview Mode](preview-mode.md) |

## Lifecycle

### `agentctl create <name>`

Creates an instance without starting it.

| Flag | Purpose |
|---|---|
| `--image <ref>` | Image reference (mutually exclusive with `--spec`) |
| `--spec <file>` | Path to a Spec YAML document (see [Profile Schema](../reference/profile-schema.md)) |
| `--profile <name>` | Named profile to apply (repeatable) |
| `--allow <domain[:port,port]>` | Egress allowlist entry (repeatable) |
| `--deny-lan` / `--allow-lan` | Block (default) or allow LAN egress |
| `--no-network-policy` | Apply no network ACL at all (unrestricted egress) — an explicit, loudly-warned escape hatch for debugging a sandbox that has locked itself out; see [Troubleshooting](troubleshooting.md#nothing-in-the-sandbox-can-reach-the-network--curl-fails-during-an-agent-install) |
| `--port <host:guest[/proto]>` | Publish a port (repeatable) |
| `--mount <hostPath[:guestPath][:w\|:ro]>` | Expose a host directory, read-only unless `:w` (repeatable) |
| `--mount-only <spec>` | Like `--mount`, but replaces the profile's mounts instead of adding to them |
| `--mount-none` | Expose no host directories at all |
| `--mount-writable` | Make every mount writable |
| `--cpu-cores`, `--memory`, `--disk-size` | Resource overrides |
| `--agent <name>` | Just-in-time install a coding agent (`claude`, `codex`, `cursor`, `gemini`, `opencode`, `pi`) — implies `start`; see [Agent Provisioning](agent-provisioning.md) |
| `--package <name>` | Install a tool into the guest during create, before any `--agent` install (repeatable). Portable names — run `agentctl profile packages` for the table. Requesting packages defers the network ACL until after they're installed; see [Packages](../reference/profile-schema.md#packages) |

Supports `--preview`/`--dry-run` (see [Preview Mode](preview-mode.md)); with
`--agent` set, preview only shows the `create` command itself — the implied
`start` and the install step aren't previewed and don't run under
`--preview` either, since preview returns before `create` actually executes.

### `agentctl start <name>`

Starts a stopped instance, and is also where a sandbox is rebound to a
different project — mounts are a per-boot property.

| Flag | Purpose |
|---|---|
| `--mount`, `--mount-only`, `--mount-none`, `--mount-writable` | Same as on `create`; rebinds the instance's host directories for this boot onward |
| `--profile <name>` | Named profile whose `mountPolicy` constrains `--mount` (repeatable) |

Mounts are **sticky**: passing no mount flag reuses whatever was applied last,
rather than resetting the instance to its profile's defaults. The active set is
printed on every start. See
[Host filesystem access](profiles-and-policies.md#host-filesystem-access).

### `agentctl stop <name> [--force] [--timeout <secs>]` / `agentctl delete <name> [--force]`

Standard lifecycle transitions. Both support `--preview`/`--dry-run`, as does
`start`.

### `agentctl list` (alias `ls`) / `agentctl status <name>`

Show instances. `status` shows one instance's detail (IPs, image, profiles,
created time).

## Interactive access

### `agentctl shell <name> [--root]`

Opens an interactive session, as the instance's provisioned non-root user by
default (see [Profiles & Policies](profiles-and-policies.md#default-non-root-user)).
Pass `--root` for a root shell instead. (`login` also works, as a hidden
alias, for anyone used to that verb from an earlier design — but `shell` is
the documented name, since "login" implies credential auth against a remote
account, which this isn't.) Supports `--preview`/`--dry-run`.

### `agentctl exec <name> -- <command...>` / `agentctl exec --root <name> -- <command...>`

Runs one command and exits with its exit code, as the provisioned non-root
user by default. **`--root` must come before `<name>`**, not after — `exec`
turns off flag/positional interleaving past the first positional argument
so that flags meant for the *inner* command (e.g. `exec demo -- ls --root`)
are never mistaken for agentctl's own. Supports `--preview`/`--dry-run`.

### `agentctl view <name> [--read-only]`

Opens a console/display viewer. See [Viewing a Sandbox](view-and-console.md)
for why this never does raw X11 forwarding. Supports `--preview`/`--dry-run`.

## Images

### `agentctl image pull <ref>` / `agentctl image build <output-name> --base <ref> [--package <name>]...`

Pull a pre-baked image, or build one just-in-time from a base image plus a
cloud-init package list. `pull` supports `--preview`/`--dry-run`.

`image build --package` and `create --package` are different mechanisms
with the same spelling, and which one you want depends on how often you'll
use the result. `image build` bakes packages into a reusable image via
cloud-init — pay the install cost once, then every `create` from that image
starts with the tools already there. `create --package` installs into one
sandbox at create time, against whatever base image you named. `image
build` takes literal distro package names; `create --package` takes the
portable names below.

## Profiles

### `agentctl profile list` / `agentctl profile show <name>` / `agentctl profile set <name>`

List available profiles (built-in `default`, `strict`, `terminal` and `gui`
plus any file-based ones), print one's resolved contents, or set the default
profile applied when `create` is given no `--profile`/`--spec`.

### `agentctl profile packages`

Prints the tool names usable in a profile's `packages:` list (and with
`create --package`), alongside the real package each one installs under
apt, dnf and apk. Supports `--json`.

A name not in this table is passed to the guest's package manager verbatim
— useful when you know your guest, but it ties the profile to one
distribution.

## Configuration

### `agentctl config set <key>=<value>` / `agentctl config get [key]`

Keys: `provider`, `profileDir`, `defaultProfile`.

## Logs

### `agentctl logs <name> [--network] [--exec] [--follow]`

Shows audit-relevant activity. Structured, correlated observability
(eBPF/ETW-based) is a larger, separate effort not built yet — this command
currently surfaces whatever the active provider natively exposes, which for
this milestone is nothing (`UnderDevelopment` on every provider). See
[Security & Threat Model](../admin/security-model.md).
