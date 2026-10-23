# Agent Provisioning

`agentctl create --agent=<name>` just-in-time installs a coding agent into a
freshly created sandbox, instead of you hand-typing its install command
after `shell`-ing in.

```console
$ agentctl create demo --image=images:ubuntu/24.04 --agent=claude
agentctl: installing claude...
...
$ agentctl shell demo
$ claude --version
```

## What `--agent` does

1. Looks up `<name>` in agentctl's built-in registry (see below). An
   unrecognized name fails immediately, before anything is created, and
   lists the valid names.
2. Merges that agent's required egress allow-domains into the instance's
   network policy, so the install and the agent's own runtime API calls
   aren't blocked by the default-deny egress policy — no separate `--allow`
   needed for the common case.
3. Sets the instance's [default non-root user](profiles-and-policies.md#default-non-root-user)
   to the agent's name (e.g. `--agent=claude` creates and runs as a `claude`
   user), unless you're already relying on a different default elsewhere.
4. Because installing requires a *running*, agent-ready instance while
   `create` normally leaves the instance stopped, `--agent` implies
   `start` — you get back a running instance, not a stopped one.
5. Runs the agent's install command as that non-root user, streaming its
   output to your terminal. A curl installer is downloaded to a file and
   run only if the download succeeded, so a blocked download fails the
   install instead of being recorded as one.
   Once it has succeeded, agentctl puts the installed agent on `PATH`
   through `~/.profile` (unless it is already there), so `agentctl shell`
   finds it by name.

## Built-in registry

| `--agent` value | Installs | Host API key it expects | Egress allowed (port 443) |
|---|---|---|---|
| `claude` | Claude Code | `ANTHROPIC_API_KEY` | `claude.ai`, `downloads.claude.ai`, `platform.claude.com`, `api.anthropic.com`, `statsig.anthropic.com` |
| `codex` | OpenAI Codex CLI | `OPENAI_API_KEY` | `chatgpt.com`, `releases.openai.com`, `auth.openai.com`, `api.openai.com` |
| `cursor` | Cursor CLI (`cursor-agent`) | `CURSOR_API_KEY` | `cursor.com`, `api2.cursor.sh` |
| `gemini` | Gemini CLI | `GEMINI_API_KEY` | `registry.npmjs.org`, `generativelanguage.googleapis.com`, `accounts.google.com`, `oauth2.googleapis.com` |
| `opencode` | opencode | *(multi-provider — none)* | `opencode.ai`, `github.com`, `api.github.com`, `release-assets.githubusercontent.com`, `models.opencode.ai` |
| `pi` | pi | `ANTHROPIC_API_KEY` *(default provider)* | `pi.dev`, `registry.npmjs.org`, `nodejs.org`, `api.anthropic.com` |

Each list covers what the installer itself fetches — install scripts
typically download the actual program from a *different* host, which is
why there's more here than one domain per agent — plus what the agent needs
to sign in and talk to its default model provider:

- **claude**: `claude.ai/install.sh` downloads Claude Code from
  `downloads.claude.ai` (also where its auto-updates come from). Signing in
  with either a claude.ai or a Console account exchanges and refreshes its
  OAuth token with `platform.claude.com`; the API itself is
  `api.anthropic.com`. Source: Claude Code's
  [network access requirements](https://code.claude.com/docs/en/network-config#network-access-requirements).
- **codex**: the installer downloads from `releases.openai.com`; signing in
  with ChatGPT goes through `auth.openai.com`, ChatGPT-plan usage through
  `chatgpt.com`, API-key usage through `api.openai.com`.
- **cursor**: the install script comes from `cursor.com`, and
  `api2.cursor.sh` carries most API requests (see the
  [note below](#cursor-may-need-more-domains-than-the-table-lists)).
- **gemini**: installs from npm (`registry.npmjs.org`); the Gemini API is
  `generativelanguage.googleapis.com`, and Google sign-in uses
  `accounts.google.com` and `oauth2.googleapis.com`.
- **opencode**: the installer looks the latest release up on
  `api.github.com` and downloads it from `github.com`, which redirects to
  `release-assets.githubusercontent.com`; opencode fetches its model
  catalog from `models.opencode.ai` at startup.
- **pi**: the installer installs pi from the npm registry and needs
  Node.js 22.19.0 or newer. It only offers to install Node itself when it
  has a terminal, which it never has under agentctl, so agentctl provides
  Node first. If `node -v` already reports 22.19.0 or newer, nothing
  changes. Otherwise, on a glibc guest (Ubuntu, Debian, Fedora) agentctl
  installs the latest Node 22 LTS release from `nodejs.org/dist/latest-v22.x`,
  verifies it against that release's `SHASUMS256.txt` and unpacks it into
  `/usr/local`; on Alpine it runs `apk add nodejs npm` and fails with the
  version it found if that is older than 22.19.0. Without passwordless
  sudo, the nodejs.org build goes into `~/.local/node` instead, and a line
  putting `~/.local/node/bin` on `PATH` is added to `~/.profile`. Then pi's
  installer runs unchanged, and agentctl puts pi's `~/.pi/agent/bin` on
  `PATH` the same way, via `~/.profile`, so `agentctl shell` finds `pi` by
  name. A failure prints a line starting
  `agentctl: pi:`, and `agentctl start <name>` retries the install.
  pi also wants ripgrep and fd, and downloads them from `github.com` at
  first use if they are missing, which the egress policy refuses; so
  before pi's installer agentctl installs them from the distribution
  (`apt-get`, `apk` or `dnf`) when the instance allows that package source
  (`--allow-preset apt` on Debian/Ubuntu, `--allow-preset apk` on Alpine),
  and otherwise prints one `agentctl: pi:` line saying so and carries on,
  leaving pi without them.

Every host is named outright, never as a wildcard: Lima's egress proxy
would match a `*.example.com` entry against each hostname, but Incus
resolves only the wildcard's apex domain (see
[Profiles & Policies](profiles-and-policies.md#egress-allowlist)), so a
wildcard would make the same agent's allowlist mean different things on
the two backends.

`opencode` and `pi` are explicitly multi-provider tools: `opencode` connects
to whichever model backend you configure inside the sandbox, and `pi`
defaults to Anthropic but also supports others. Their table entries above
only guarantee installing them (`pi` additionally covers its
default provider) — if you point either at a different
model provider, extend the allowlist the same way you would for anything
else: `agentctl create demo --agent=opencode --allow=<your-provider-domain>:443`.

`--agent` only opens what the agent needs. If it will also install
packages or clone repositories, allow those sources too — e.g.
`--allow-preset=github,pypi` (see
[Profiles & Policies](profiles-and-policies.md#allowing-package-sources-and-git-hosts)).

## Allowing an agent without `--agent`

Each agent's hosts are also an allow preset of the same name, so
`--allow-preset=claude` (or `allowPresets: [claude]` in a profile) allows
exactly what `--agent=claude` does without installing anything — for an
image that already has the agent, installing it some other way, or an
org profile meant for Claude Code work:

```console
$ agentctl create demo --image=images:ubuntu/24.04 --allow-preset=claude,apt,github
$ agentctl start demo
$ agentctl exec demo -- sh -c 'curl -fsSL https://claude.ai/install.sh | bash'
```

## Tools the agent needs, that the image doesn't have

`--agent` installs the agent and nothing else. Base images are minimal —
`images:ubuntu/24.04` has no `git`, no compiler, no editor — so an agent
that can run may still be unable to do much.

Install them in the same `create`, with `--package` or a profile that
bundles them:

```console
$ agentctl create demo --image=images:fedora/44 --profile=terminal --agent=claude
```

Packages are installed **before** the agent, so an agent whose installer
needs a tool present (`--agent=gemini` needs `npm`) can get it the same
way:

```console
$ agentctl create demo --image=images:ubuntu/24.04 --package=nodejs --package=npm --agent=gemini
```

See [Tools in the guest](profiles-and-policies.md#tools-in-the-guest-packages-and-the-terminalgui-tiers).

### `gemini` needs Node.js already in the guest

Google documents no curl-based installer for Gemini CLI — only npm, npx and
Homebrew — so `--agent=gemini` runs `npm install -g @google/gemini-cli` and
**requires Node.js 20+ to already be present in the image**. None of the base
images used throughout these docs (`images:ubuntu/24.04`, `images:alpine/edge`,
`template://ubuntu-lts`) ship Node, so on a bare image this fails with
`npm: command not found` rather than installing anything.

agentctl deliberately does not bootstrap Node for you: that would be inventing
an install path the agent's own documentation doesn't describe. Ask for it
explicitly instead — `--package=nodejs --package=npm` on the same `create`
(installed before the agent, so the install finds it), or use an image that
already has it. If you've already hit the failure, install into the existing
instance — `agentctl exec --root demo -- apt-get install -y nodejs npm` —
and `agentctl start demo` retries the pending install automatically (see
below).

### `cursor` may need more domains than the table lists

Cursor spreads runtime traffic across more hosts than the other agents. The
entry above covers install plus the primary API path (`api2.cursor.sh`), but
Cursor's own network-configuration docs also name `api3`/`api4`/`api5`,
`repo42.cursor.sh`, the `*.authentication.cursor.sh` hosts and
`*.cursor-cdn.com` as needed in some setups. If something fails to connect,
extend the allowlist the same way as for a multi-provider agent, naming
the hosts — `agentctl create demo --agent=cursor --allow=api3.cursor.sh:443`.
(A wildcard such as `--allow='*.cursor.sh:443'` covers them all on Lima,
but on Incus it only allows `cursor.sh` itself.)

## What it doesn't do (yet)

Forwarding a host API key (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, ...) into
the guest is a separate, not-yet-built mechanism — `--agent` installs the
agent binary, it doesn't configure credentials for you. Set the key inside
the sandbox the same way you would on any machine (an env var, a config
file, whatever the agent's own docs say) after `shell`-ing in.

## If install fails partway

A transient failure during install (e.g. a flaky network mid-`curl`) isn't
silently swallowed: `create --agent=<name>` returns a real error, and the
instance is left running with the failure recorded. The next
`agentctl start <name>` (even a completely ordinary one, with no `--agent`
flag) detects the incomplete install and retries it automatically before
doing anything else — see
[Troubleshooting](troubleshooting.md#agent-install-fails-or-never-finishes)
for details. A plain `create` (no `--agent`) is entirely unaffected by any
of this.

Where that "was an install requested, did it finish" state actually lives
differs per provider: on Incus it's recorded on the instance itself (via
`incus config get <name> user.agentctl-agent`), so the backend's own
per-instance config stays the single source of truth. Lima has no
equivalent per-instance custom-metadata primitive, so on Lima this state
lives in a small local file instead, `~/.config/agentctl/lima-state.yaml`
(or next to whatever `AGENTCTL_CONFIG` points at) — see
`internal/provider/lima/state.go`. This only matters if you're inspecting
or scripting around this state directly; `agentctl start`'s retry
behavior above is identical either way.
