# Deployment Model

`agentctl` follows a **local tool, centrally distributed policy** model:

- The `agentctl` binary and the actual sandbox VM/container run **on each
  user's own machine**. There's no shared backend infrastructure to operate,
  and compute cost scales with users trivially (each person's laptop does its
  own work).
- **Security policy is centralized.** Admins author profile YAML files (see
  [Distributing Profiles Org-Wide](distributing-profiles.md)) in a shared
  location — a git repo, a network share, whatever fits — and point every
  user's `agentctl` at it via `agentctl config set profileDir=<path>`.

This gets you org-wide consistency on the things that matter (egress
allowlists, LAN blocking, resource caps) without standing up a central
provisioning service, and without trusting individual users to author their
own security policy from scratch.

## Why not a centrally-hosted service?

A shared backend (think self-hosted E2B/Microsoft) gives the strongest
central control and the least trust placed in end-user machines, but it's
heavier to operate and turns "spin up a sandbox" into a request against
shared infrastructure with its own capacity and availability concerns. Given
the target audience — many users, several OSes, Linux-primary but not
Linux-only — the local-tool model is the better fit for this project. Nothing
here rules out building a centrally-hosted mode later if requirements change.

## Rollout checklist

1. Stand up a shared profile directory (see
   [Distributing Profiles Org-Wide](distributing-profiles.md)).
2. Install the right backend per OS (see the provider setup pages) —
   the Incus/Linux and Lima/macOS paths are both fully implemented for
   lifecycle commands; Windows users will see `UnderDevelopment` responses
   until Hyper-V lands (check the
   [capability matrix](capability-matrix.md)).
3. Have each user run:
   ```console
   $ agentctl config set provider=<incus|lima|hyperv>
   $ agentctl config set profileDir=<shared path>
   $ agentctl config set defaultProfile=<org-default-profile-name>
   ```
4. Point users at the [Quickstart](../user/quickstart.md).
