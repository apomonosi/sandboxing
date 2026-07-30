# Quickstart

This walks through the full lifecycle of a sandbox on the Incus backend. It
assumes you've already run through [Installation](installation.md).

## Create

```console
$ agentctl create my-agent --image=images:ubuntu/24.04 --profile=default
Name:      my-agent
Provider:  incus
Status:    stopped
Image:
Profiles:  default
IPs:
```

This creates the instance but doesn't start it. `--profile=default` applies
the built-in `default` profile: default-deny egress with a small allowlist,
RFC1918/link-local blocked, 2 CPU cores, 4GiB memory, 20GiB disk. See
[Authoring Profiles & Policies](profiles-and-policies.md) to customize this.

## Start

```console
$ agentctl start my-agent
```

## Get a shell

```console
$ agentctl shell my-agent
```

This opens an interactive session inside the sandbox. Do whatever risky,
agent-driven work you needed the isolation for.

## Confirm the network policy is doing its job

From inside the shell (or via `agentctl exec my-agent -- ...`):

```console
$ curl -s --connect-timeout 3 http://<some-other-machine-on-your-lan>/
curl: (28) Connection timed out
```

That timeout is `--deny-lan` working as intended — the sandbox cannot reach
other devices on your network by default.

## Stop and clean up

```console
$ agentctl stop my-agent
$ agentctl delete my-agent
```

## See exactly what a command would do, before it does it

```console
$ agentctl --preview create my-agent --image=images:ubuntu/24.04 --profile=default
incus init images:ubuntu/24.04 my-agent --vm -p default -c limits.cpu=2 -c limits.memory=4GiB
...
```

Nothing runs — this just prints the underlying `incus` commands. See
[Preview Mode](preview-mode.md).

## One-shot commands without a full shell

```console
$ agentctl exec my-agent -- python3 script.py
```

## Watching the sandbox's display

If your agent drives a GUI (e.g. browser automation), see
[Viewing a Sandbox](view-and-console.md) for `agentctl view` — note it
deliberately does not do raw X11 forwarding.
