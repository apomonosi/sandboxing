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
   output to your terminal.

## Built-in registry

| `--agent` value | Installs | Host API key it expects | Egress allowed |
|---|---|---|---|
| `claude` | Claude Code | `ANTHROPIC_API_KEY` | `claude.ai`, `downloads.claude.ai`, `platform.claude.com`, `api.anthropic.com`, `statsig.anthropic.com` |
| `codex` | OpenAI Codex CLI | `OPENAI_API_KEY` | `chatgpt.com`, `api.openai.com` |
| `cursor` | Cursor CLI (`cursor-agent`) | `CURSOR_API_KEY` | `cursor.com`, `api2.cursor.sh` |
| `gemini` | Gemini CLI | `GEMINI_API_KEY` | `registry.npmjs.org`, `generativelanguage.googleapis.com`, `accounts.google.com`, `oauth2.googleapis.com` |
| `opencode` | opencode | *(multi-provider — none)* | `opencode.ai` |
| `pi` | pi | `ANTHROPIC_API_KEY` *(default provider)* | `pi.dev`, `api.anthropic.com` |

`opencode` and `pi` are explicitly multi-provider tools: `opencode` connects
to whichever model backend you configure inside the sandbox, and `pi`
defaults to Anthropic but also supports others. Their table entries above
only guarantee the *install* domain (`pi` additionally covers its
default-provider runtime domain) — if you point either at a different
model provider, extend the allowlist the same way you would for anything
else: `agentctl create demo --agent=opencode --allow=<your-provider-domain>:443`.

### `gemini` needs Node.js already in the guest

Google documents no curl-based installer for Gemini CLI — only npm, npx and
Homebrew — so `--agent=gemini` runs `npm install -g @google/gemini-cli` and
**requires Node.js 20+ to already be present in the image**. None of the base
images used throughout these docs (`images:ubuntu/24.04`, `images:alpine/edge`,
`template://ubuntu-lts`) ship Node, so on a bare image this fails with
`npm: command not found` rather than installing anything.

agentctl deliberately does not bootstrap Node for you: that would be inventing
an install path the agent's own documentation doesn't describe. Install Node
first — `agentctl exec --root demo -- apt-get install -y nodejs npm`, or use an
image that already has it — then `agentctl start demo` retries the pending
install automatically (see below).

### `cursor` may need more domains than the table lists

Cursor spreads runtime traffic across more hosts than the other agents. The
entry above covers install plus the primary API path (`api2.cursor.sh`), but
Cursor's own network-configuration docs also name `api3`/`api4`/`api5`,
`repo42.cursor.sh`, the `*.authentication.cursor.sh` hosts and
`*.cursor-cdn.com` as needed in some setups. If something fails to connect,
extend the allowlist the same way as for a multi-provider agent:
`agentctl create demo --agent=cursor --allow='*.cursor.sh:443'`.

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
