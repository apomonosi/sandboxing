# Lima Image Sourcing — Design Notes (Not Yet Implemented)

**Status:** Brainstorm/design discussion only. Nothing described here has
been implemented, no code has been written, and no decision has been made
on the open questions at the bottom. This file exists purely to preserve
the discussion so it can be picked up again later without re-deriving it.

## The question

Incus's `--image` resolves against **simplestreams remotes** — a real
server-side protocol (`images:`, `ubuntu:`, or a private one an org stands
up) that agentctl can query, list, and pull from ahead of time
(`internal/provider/incus/translate.go`'s `buildImagePullArgs`, `incus
image copy`). Does Lima have an equivalent, and if not, can agentctl
self-host something comparable?

## Finding: no true parallel exists

Lima has nothing structurally equivalent to simplestreams. A Lima template
is a self-contained YAML file that hardcodes a URL straight to an upstream
vendor's cloud image (`cloud-images.ubuntu.com/...`, Fedora's official
cloud mirror, Debian's, etc.), usually with a pinned SHA256 digest for
integrity. `limactl start template://ubuntu-lts` resolves against Lima's
own bundled/embedded template set (shipped inside the `limactl` binary,
mirrored from the `lima-vm/lima` GitHub repo) — there is no queryable
catalog *server* behind `template://`, just a name resolving to one of
those embedded YAML files.

This actually makes self-hosting easier, not harder: there's less protocol
to replicate, just files to host.

## Self-hosting decomposes into two independent, already-possible pieces

`spec.Image` is a bare, unvalidated string forwarded straight through to
`limactl create` (`internal/provider/provider.go:87`'s doc comment already
anticipated this: "OCI/simplestreams ref, Lima template, or Hyper-V VHDX
ref"; see `internal/provider/lima/translate.go`'s `buildCreateArgs`). So
both of the following already work today, with **zero agentctl code
changes**:

1. **Template source** — where the YAML itself comes from. `--image` can
   already be a local file path or a full URL to a YAML template, e.g.
   `--image=https://internal.corp.example/lima-templates/hardened-ubuntu.yaml`.
2. **Image source** — where the *cloud image* that template's `images:`
   list points at actually lives. That's just a URL + digest inside the
   YAML; nothing stops it from pointing at an internally-hosted, org-
   rebuilt/hardened image instead of Canonical's or Fedora's servers.

## Where an actual feature would live

Not "make this possible" — it already is. The value would be a
governance/convenience layer on top, comparable to what a private
simplestreams remote gives Incus users:

- A configured default template base URL/locator (mirroring how a profile
  can set a default provider today), so users type `--image=hardened-ubuntu`
  instead of a full internal URL.
- An `agentctl image list` that enumerates an internal catalog rather than
  requiring people to know filenames.
- Digest-pinning enforcement — refuse to boot a template whose image URL
  has no matching `checksum:` entry. Given how seriously the rest of this
  project treats supply-chain/network concerns (the non-root-user work,
  the network ACL model, the proxy design in `proxy.plan.md`), this is
  probably the single most important piece if this ever gets built for
  real, not an afterthought.

## Two things worth staying explicit about

1. **This doesn't touch the `image.pull` = `NotAvailable` capability
   finding from the Lima backend implementation.** That entry is about
   there being no "pull ahead of time into a local named store" step — a
   self-hosted library changes *where* `create`/`start` points, not
   *whether* a separate pull step exists. (Side note, unverified: `limactl`
   appears to cache a downloaded image locally after first use, so only
   the first `create` against a given template pays the download cost —
   but that's `limactl`'s own cache behavior, not something agentctl
   controls or should rely on as a guarantee without confirming it live.)
2. **This is a host-side fetch, not a guest-side one — a different trust
   boundary than `proxy.plan.md`.** Lima downloads the base image on the
   macOS *host*, before the guest ever boots. None of agentctl's guest
   network policy (`denyLAN`, `--allow`, the proxy design) governs that
   fetch at all — it's the host's own network path. If the *host* sits
   behind a corporate proxy, that's `limactl`'s/Go's own
   `http_proxy`/`https_proxy` env-var handling on the host, unrelated to
   anything agentctl configures inside the guest. Keep "host fetches the
   image" and "guest reaches the internet" as clearly separate concerns in
   any future design — conflating them would be a mistake.

## Open questions to resolve before this becomes an implementation plan

1. Is a convenience/catalog layer (named aliases, `agentctl image list`)
   worth building at all, given the underlying capability (point `--image`
   at an internal URL) already works with no code changes? Or is the real
   ask just documentation — a guide for orgs on how to host their own
   templates/images and point `--image` at them?
2. If a catalog layer is worth building, does it live in `agentctl` itself
   (a new subcommand + config), or as an org-authored static file (a
   simple JSON/YAML index an admin hosts and `agentctl` fetches from a
   configured locator URL, cheaper to build than a real server)?
3. Digest-pinning enforcement: opt-in, opt-out, or mandatory whenever a
   non-embedded/non-default template is used?
4. Should this eventually generalize across providers (a single
   `internal/image` catalog concept usable by Incus's simplestreams and a
   Lima template/URL locator alike), or is that premature given only one
   provider (Lima) actually lacks a real remote-image protocol today?

## Where this fits once resolved

Once the open questions above are answered, this should go through the
same plan-mode workflow the Lima backend and non-root-user/JIT-agent
features did: explore existing patterns (`internal/image`,
`internal/provider/lima/translate.go`, `profile/schema.go`), design a
concrete file-by-file plan, and get it reviewed before any code is
written.
