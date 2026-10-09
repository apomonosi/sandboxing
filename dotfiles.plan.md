# Guest Shell Configuration & Dotfiles — Design Notes (Not Yet Implemented)

**Status:** Brainstorm/design discussion only. Nothing described here has been
implemented and no decision has been made on the open questions at the
bottom. This file exists purely to preserve the discussion so it can be
picked up again later without re-deriving it. Same shape and status as
`proxy.plan.md`.

## What prompted this

Two things, in order.

The first is a confirmed bug with a one-character fix, carried here only
because it is the thread everything below hangs off: `bootstrap-user.sh`'s
`populate_skel` skips a destination that merely *exists*:

```sh
if [ -e "$dest" ]; then
    continue
fi
```

On a live Fedora 44 guest, `/home/agent/.bashrc` existed at **0 bytes**
(`d41d8cd98f00b204e9800998ecf8427e` — the MD5 of an empty file) while
`/etc/skel/.bashrc` had content. `-e` cannot tell a deliberately-emptied
file from one that was never populated, and at bootstrap time — before any
user or installer has touched the home directory — a 0-byte dotfile is
almost certainly the latter. The fix is `-s` (non-empty) instead of `-e`,
which still never overwrites content but does fill in empties. **Agreed, not
yet written.** What created the empty file in the first place is still
unknown; the decisive test is whether this image's `useradd -m` populates
skel at all:

```
incus exec <name> -- useradd -m -s /bin/bash skeltest
incus exec <name> -- md5sum /home/skeltest/.bashrc
```

The second, and the actual subject of this file: once agentctl is writing
into a guest's home directory at all, the question is what *else* belongs
there for an agentic shell, and whether users should have to post-configure
every sandbox by hand.

## One finding that shapes most of the options below

There is **no portable drop-in directory for non-login interactive shells.**

| Location | Fedora | Debian/Ubuntu | Alpine | Covers |
|---|---|---|---|---|
| `/etc/profile.d/*.sh` | yes | yes | yes (ash) | login only |
| `~/.bashrc.d/` | **yes** — the skel `.bashrc` already loops over it | no | no | non-login too |
| `/etc/bashrc` | yes | — (`/etc/bash.bashrc`) | — | non-login, not portable |

Fedora's default skel `.bashrc` genuinely ships this:

```sh
if [ -d ~/.bashrc.d ]; then
	for rc in ~/.bashrc.d/*; do
		if [ -f "$rc" ]; then
			. "$rc"
		fi
	done
fi
```

So `~/.bashrc.d` is Fedora-idiomatic but not something to rely on
cross-distro, and `/etc/profile.d` — the one portable location — misses
exactly the shells that an agent spawns programmatically. Any mechanism here
has to cover both tiers deliberately rather than pick one and hope.

---

## A. The drop-in mechanism itself

1. **`/etc/profile.d/agentctl.sh`, written by the bootstrap script.** One
   agentctl-owned file, system-wide, portable across all three families,
   covers login shells and root. Touches none of the user's own dotfiles.

2. **A single guarded include appended to `~/.bashrc`** for the non-login
   gap — `# >>> agentctl >>> … # <<< agentctl <<<`, idempotent and
   removable. The conda/rustup pattern. This would be the *only* place
   agentctl edits a file the user might also edit, which is worth keeping
   true.

3. **`~/.bashrc.d/` as the user-extensible tier**, created by agentctl and
   sourced by (2) on every distro rather than only Fedora. Numbered files,
   with agentctl owning `00-`–`09-` and everything else left to the user.

## B. Content worth putting in it

4. **`PATH` with `~/.local/bin` and `~/bin`.** Belt and braces, so non-login
   shells match what `exec` already sets explicitly (see `loginPath` in
   `internal/provider/incus/translate.go`).

5. **A sandbox marker in `PS1`** (e.g. `(sandbox:remove)`) plus
   `AGENTCTL_SANDBOX` / `AGENTCTL_PROVIDER` environment variables. Cheap,
   and it means a sandbox shell can never be mistaken for a host shell in a
   stack of terminals. Ranked higher than its size suggests.

6. **`AGENTCTL_WORKSPACE=/workspace`, and `cd` there on login.** The mount is
   the point of the sandbox; landing in `~` is a small daily tax.

7. **Locale and editor defaults** (`LANG=C.UTF-8`, `EDITOR`). Minimal images
   frequently ship no locale, which surfaces as Python/Node unicode errors a
   long way from the cause.

8. **Proxy environment variables** (`http_proxy`, `https_proxy`, `no_proxy`).
   This is the natural landing site for `proxy.plan.md`'s guest half and
   would make that work substantially smaller. Worth deciding here rather
   than there — see the open questions.

9. **Bash history in `/workspace`** (`HISTFILE`, `HISTSIZE`, timestamps).
   Survives VM recreation, and doubles as a record of what the agent ran.

10. **Per-agent telemetry and auto-update opt-outs.** Agents that
    self-update behave badly behind a default-deny ACL — failing noisily, or
    silently, at startup. A registry field for "environment variables this
    agent needs in order to behave in a sandbox" fits `agent.Spec`'s
    existing shape.

## C. Making the sandbox's constraints legible from inside

11. **A `sandbox-info` helper on the guest `PATH`** printing the active
    allowlist, `denyLAN`, mounts and proxy. Directly addresses what cost the
    most during the network debugging — *"no DNS inside that VM makes
    debugging difficult"*. Requires the resolved policy to be written into
    the guest at create time (a small JSON under `/etc/agentctl/`), which is
    a useful primitive in its own right.

12. **A `DEBUG` trap or `PROMPT_COMMAND` command log.** This is a partial
    implementation of `logs.exec`, which is `UnderDevelopment` on *every*
    provider today. Cheap, and it must be honest about its limits —
    trivially bypassed, so an audit aid rather than a control.

13. **A failure hint on blocked egress** — trapping `curl`/`git` exit codes
    and pointing at the allowlist. Listed in order to argue *against* it:
    too magical, fires on unrelated failures, and wraps the very tools the
    agent calls programmatically.

## D. Dotfiles proper

14. **`--mount ~/.dotfiles:/home/agent/.dotfiles:ro` already works today.**
    Establish whether a dedicated mechanism beats documenting this one line
    before building anything at all.

15. **`--dotfiles <host-dir>`, copied into the guest at create.** Simple, no
    network, works under the default-deny policy. A copy rather than a
    mount, so the agent cannot write back to the host's dotfiles.

16. **`--dotfiles <git-url>`, cloned in the guest.** What Codespaces does.
    But it needs the ACL to allow the git host *before* the policy has been
    tuned, and a private repo needs credentials — a lot of machinery for the
    same result as (15).

17. **`dotfiles:` as a profile field**, so an organization can ship a house
    shell configuration. Raises the same additive-merge question `packages:`
    already answered.

18. **An `install.sh` / `bootstrap` convention inside the dotfiles
    directory**, chezmoi/stow style. Flexible, and the point at which this
    stops being configuration and becomes arbitrary code execution.

## E. Concerns to settle before anything in D is built

19. **Dotfiles are executable code that runs as the agent user on every
    shell.** An org profile shipping dotfiles is shipping code through a
    channel whose current job is *restricting* things. That deserves its own
    decision, not one inherited from `packages:`.

20. **A writable dotfiles path is an agent persistence mechanism.** If the
    agent can write `~/.bashrc.d/`, it survives into every future shell in
    that sandbox. Inherent to having the directory at all, but it argues for
    agentctl's own files being root-owned `0644` with the user tier as the
    only writable one.

21. **Never copy the host `~` wholesale.** That is precisely what
    `mountPolicy`'s `allowHome: false` exists to prevent, and a
    `--dotfiles ~` would walk straight around it. Whatever this becomes
    needs the same `ResolveMounts` treatment every other host path gets.

22. **Credentials are a separate mechanism, not this one.** API keys and SSH
    keys in `.bashrc` end up in environment dumps, `ps` output, and the
    agent's own context. The acknowledged `--agent` gap (forwarding a host
    API key) wants `0600` files or a credential helper, and SSH agent
    forwarding is a signing oracle for whatever the agent asks it to sign.

23. **git identity** (`user.name` / `user.email`) is the one piece of
    "dotfile" everyone needs and nobody wants to configure — commits from a
    sandbox are otherwise authored by `agent@fedora`. Small enough to
    deserve its own flag rather than waiting for a general mechanism.

---

## Suggested sequencing

- **First, as one small change with the `-s` fix:** 1, 2, 3 (the mechanism)
  + 4, 5, 7 (uncontroversial content) + 23 (git identity).
- **Then, on its own:** 11. It pays for itself the next time a sandbox locks
  itself out.
- **Then take 8 to the proxy discussion**, where it belongs.
- **Leave 14–18 until (14) has been tried and found insufficient.**

## Open questions to resolve before this becomes an implementation plan

1. **Does agentctl edit `~/.bashrc` at all?** (2) is the only proposal here
   that writes into a file the user also owns. The alternative is
   `/etc/profile.d` only, accepting that non-login interactive shells stay
   unconfigured on Debian and Alpine. This is the central design decision.
2. **Who owns the policy snapshot in the guest?** (11) needs the resolved
   policy written to `/etc/agentctl/`. That is a new artifact with its own
   staleness problem — `start --mount` can change mounts after create, so a
   snapshot written at create time can lie.
3. **Does `8` belong here or in `proxy.plan.md`?** Both files would
   otherwise describe the same guest-side writes.
4. **Is a dedicated dotfiles mechanism warranted at all**, given (14)? This
   should be answered empirically, not by design argument.
5. **Does `packages:`'s merge semantic transfer to `dotfiles:`?** (19)
   suggests not: additive accumulation is right for software that adds
   capability, but shipping executable shell code through an org-distributed
   profile is a different kind of act.

## Where this fits once resolved

Same route as the Lima backend and the non-root-user/JIT-agent features:
explore the existing patterns (`bootstrap-user.sh`, `translate.go`'s
`loginPath`/`buildShellArgs`, `agent.Spec`, `profile/schema.go` +
`merge.go`, `internal/packages`), design a concrete file-by-file plan, and
get it reviewed before any code is written.
