# Image → Instance → Mount: the Docker-shaped Workflow (Plan, Not Yet Implemented)

**Status:** Design plan only. No code has been written for any of this. Grew out
of a question about whether a sandbox has to be created per project, or whether
a pre-equipped template image plus disposable instances is the better model.
Companion to `filesystem-access.plan.md` (mounts) and `lima-images.plan.md`
(where template/image *content* comes from); this file is about the lifecycle
verbs.

## The mental model

The Docker analogy holds better than the microVM framing suggests, and the
existing command set already lines up with it:

| Docker | agentctl today | Notes |
|---|---|---|
| `docker pull` | `agentctl image pull <ref>` | Incus: Supported. Lima: **NotAvailable** (no local image store) |
| `docker build` | `agentctl image build <name> --base … --package …` | Exists as a command; **does nothing on any provider** — see findings |
| `docker create` | `agentctl create <name> --image=…` | Creates, does not start. Same split Docker has |
| `docker start` | `agentctl start <name>` | |
| `docker run -v host:guest` | `agentctl start --mount …` | Proposed in `filesystem-access.plan.md` |
| `docker exec` | `agentctl exec` / `agentctl shell` | |
| `docker commit` | **missing** | Incus has `incus publish`; nothing is wired to it |
| `docker rm` | `agentctl delete` | |
| `docker images` | **missing** | No `image list`/`image rm` |
| (no equivalent) | `agentctl profile` | Security policy bundle — network ACL, LAN, mounts, resources |
| (closest: `docker checkpoint`) | `agentctl snapshot` via provider | Incus: Supported. Lima: NotAvailable |

Where the analogy breaks, and it matters for expectations:

- **No layers, no build cache.** An Incus image is a single blob, not a stack of
  content-addressed layers. There is no `Dockerfile` whose seventh line rebuilds
  in isolation — changing anything means re-running the whole build. Plan on
  images being rebuilt on a schedule (nightly/weekly), not per edit.
- **Boot is seconds, not milliseconds.** These are full VMs with their own
  kernel. `create` on Incus also does a start → provision user → stop cycle
  (`incus.go`'s `Create`), so it is heavier than `docker create` — closer to
  `docker run` plus a first-boot provisioning step.
- **Disks are GB-sized, but cheap to clone.** Incus creates instances
  copy-on-write from the image, so ten instances off one 8 GB image do not cost
  80 GB. This is what makes the disposable-instance model affordable.
- **`agentctl create --profile=X` is not `docker run` with a name.** A profile
  is security policy, not image content.

## The recommended shape

Three layers with different lifetimes, which resolves the "one VM per project or
one reusable VM?" tension — the answer is neither:

| Layer | Lifetime | Holds | Verb |
|---|---|---|---|
| **Image** | weeks/months, shared across projects | toolchains, runtimes, language servers, the agent CLI | built once, `image build` / `image save` |
| **Instance** | one task, disposable | whatever the agent did | `create` + `delete` freely |
| **Mount** | one boot | the project directory | `start --mount ~/src/alpha:/workspace:w` |

The durable thing is the **image**, not the VM. That preserves the README's
"ephemeral sandbox" property — a compromised instance is deleted, not carried
into the next project — while still not reinstalling Go and Node for every task.
A long-lived reused VM gets the ergonomics at the cost of accumulating state and
carrying any compromise across projects; a rebuilt-from-image instance gets both.

## Findings in the current code

**1. `agentctl image build` is a silent no-op on Incus — and reports success.**
`internal/provider/incus/capability.go:36` marks `FeatureImageBuild` as
`Supported`, so the capability gate in `internal/cli/image.go:60` passes. But
`internal/provider/incus/incus.go:417` implements the method as:

```go
return "", provider.StatusError(p.capabilities.Get(provider.FeatureImageBuild))
```

and `StatusError` returns **`nil`** for `Supported` (`errors.go:27`). So the
command prints an empty line and exits 0, having built nothing. Every other
stub in the codebase returns a real sentinel error because its capability entry
is honestly non-Supported; this is the one place where the table claims a
capability the method does not implement, and the failure mode is the worst
kind. `docs/admin/capability-matrix.md:20` repeats the claim.

Fix regardless of anything else in this plan: either implement it, or set the
entry to `UnderDevelopment` so the gate blocks with exit code 2. A test asserting
"every `Supported` feature has a method that does not return `StatusError` on
its own capability" would prevent the class.

**2. `spec.Profiles` is passed to `incus init` as Incus profile names.**
`buildInitArgs` (`incus/translate.go:178`) emits `-p <name>` for each agentctl
profile name. Incus resolves those against its *own* profile objects, which have
no relationship to agentctl's YAML profiles. `--profile=default` works by
coincidence (Incus ships a profile called `default`); `--profile=strict` or any
org profile name will fail at `incus init` with a profile-not-found error. The
Lima backend deliberately does not do this — `lima/translate.go:63` explains that
policy is already fully resolved into `spec.Resources`/`Mounts`/`Overrides`
before `Create` is called, so there is nothing left to map. The same reasoning
applies to Incus: drop the `-p` passthrough, or introduce a separate,
explicitly-named field for "attach these *Incus* profiles too."

**3. `internal/image` is a stub.** `RenderCloudInit` emits `package_update` plus
a package list and nothing else — no users, no files, no run commands, no image
provenance. Enough for `apt install build-essential`, not enough for a real
project toolchain image.

## Proposed API

### `agentctl image build` — make it real

Keep the flag form for the trivial case, add a spec file for the real one,
matching how `create` already takes either flags or `--spec`:

```console
$ agentctl image build devbox --base images:ubuntu/24.04 --package golang --package nodejs
$ agentctl image build devbox --spec image.yaml
```

```yaml
apiVersion: agentctl.dev/v1
kind: Image
metadata:
  name: devbox
spec:
  base: images:ubuntu/24.04
  packages: [golang, nodejs, ripgrep]
  agents: [claude]              # reuse internal/agent's install specs
  runCommands:
    - "npm install -g typescript"
  files:
    - guestPath: /etc/gitconfig
      content: |
        [init]
          defaultBranch = main
```

This is the same `apiVersion`/`kind`/`metadata`/`spec` document shape as
`profile.Profile` and `spec.Spec`, strict-decoded the same way, so it needs no
new conventions. `internal/image`'s `RenderCloudInit` grows `write_files`,
`runcmd`, and the agent-install steps that `internal/agent` already describes.

Incus mechanics: `incus init` from the base with the rendered cloud-init in
`cloud-init.user-data`, start, wait for `cloud-init status --wait`, stop,
`incus publish <instance> --alias <name>`, delete the scratch instance. All
existing primitives; nothing exotic.

### `agentctl image save <instance> <image-name>` — the `docker commit` verb

This is the piece the workflow is missing, and the one that makes "equip a VM
interactively, then keep it" work:

```console
$ agentctl create scratch --image=images:ubuntu/24.04
$ agentctl start scratch && agentctl shell scratch     # install things by hand
$ agentctl image save scratch devbox                   # → local image "devbox"
$ agentctl create alpha --image=devbox                 # every later task starts here
```

Maps to `incus publish <instance> --alias <name>` (the instance must be stopped,
or `--force`). Add `FeatureImageSave` to `AllFeatures` so the completeness test
forces every provider to declare an entry.

Worth stating in the docs: an image saved this way is unreproducible — it
carries whatever the agent left behind, including anything malicious. `image
build` from a spec is the reviewable path; `image save` is the convenience path.
For an org-distributed image that distinction matters.

### `agentctl image list` / `agentctl image rm`

`incus image list --format json` / `incus image delete`. Needed simply because
once images are the durable artifact, they need to be enumerable and
garbage-collectable.

## Per-provider reality

- **Incus**: everything above maps to native primitives (`publish`, `image
  list/delete/copy`, cloud-init via `cloud-init.user-data`). This is where the
  workflow works first.
- **Lima**: no local named image store — `image.pull` is already
  `NotAvailable` for exactly this reason. A Lima "image" is a template YAML
  pointing at a cloud-image URL plus a digest. So `image save` has no native
  target: the honest options are (a) `NotAvailable` with a Plan string pointing
  at `lima-images.plan.md`'s self-hosted-template approach, or (b) export the
  instance's qcow2 disk and generate a template YAML referencing it — real, but
  a genuinely different mechanism, not a thin wrapper. Recommendation: (a) for
  v1, and lean on `create --spec` + cloud-init provisioning on Lima instead of
  pretending it has images.
- **Hyper-V**: stub; `NotAvailable` throughout.

This asymmetry is worth being loud about in the capability matrix rather than
smoothing over: on Incus the model is "build an image, clone instances from it";
on Lima it is "one template, provisioned at first boot each time."

## Where snapshots fit

Distinct from images, and already `Supported` on Incus today: `snapshot
create`/`restore` saves *one instance's* state mid-task ("checkpoint before
letting the agent refactor"), whereas an image is a reusable starting point for
many instances. The Docker analogy is `docker checkpoint` vs `docker commit`.
The CLI currently exposes neither — `Provider` has all four snapshot methods and
the Incus backend implements them, but there is no `agentctl snapshot` command
wired up in `internal/cli`. That is a cheap win for the save-my-work use case,
independent of everything else here.

## Suggested phasing

**Phase 0 — fix the lie.** Set Incus's `image.build` entry to
`UnderDevelopment` (or implement it), and add the test that stops a `Supported`
entry from pairing with an unimplemented method. Drop or rename the `-p`
profile passthrough in `buildInitArgs`.

**Phase 1 — `agentctl snapshot` command surface.** The provider methods already
exist and Incus already implements them; this is CLI wiring only.

**Phase 2 — `image save`.** `incus publish` + capability entry + docs on the
reproducibility caveat.

**Phase 3 — real `image build`.** The Image spec document, `internal/image`
cloud-init expansion, agent-install reuse, `image list`/`rm`.

## Open questions

1. **Is `image build` worth building at all, or is `image save` plus a
   documented "build your images with Packer/a CI job" enough?** Every org that
   needs reproducible images already has a pipeline; agentctl reimplementing a
   thin cloud-init builder may be the wrong layer. The counter-argument is that
   `--base` + `--package` is a genuinely useful 30-second on-ramp.
2. **Does an image carry policy?** An image built with `--agent=claude` implies
   an egress allowlist to make that agent work. Should the Image document be
   able to reference a default profile, so `create --image=devbox` picks up the
   matching policy — or does silently attaching policy to an image undermine the
   "policy is visible and explicit" property the profile design is built on?
   Leaning strongly toward: images carry *content*, profiles carry *policy*, no
   coupling.
3. **Image naming/namespacing.** `--image=devbox` must not collide with
   `images:ubuntu/24.04` (a remote simplestreams ref) or a Lima template name.
   Incus's own `local:` prefix convention probably answers this, but the
   resolution order needs writing down.
4. **Where does the guest's own build cache live?** A per-project `~/.cache`,
   `node_modules`, or Go module cache is not image content and not project
   source. Options: a second mount from the host, a persistent Incus volume
   attached across instances, or accept the cold cache on every fresh instance.
   This is the thing most likely to make disposable instances feel slow, and it
   interacts directly with `filesystem-access.plan.md`'s mount policy.
