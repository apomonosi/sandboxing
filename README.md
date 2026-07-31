# agentctl

Hardened, ephemeral sandboxes for running AI agents safely — on Linux
(Incus), macOS (Lima), and Windows (Hyper-V), defending against both host
escape and lateral attacks against other machines on the same network.

Full documentation is published at
[apomonosi.github.io/sandboxing](https://apomonosi.github.io/sandboxing/),
built from [`docs/`](docs/index.md) with [MkDocs](https://www.mkdocs.org/).
To build it locally:

```console
$ pip install mkdocs mkdocs-material
$ mkdocs serve
```

## Quick start

```console
$ go install github.com/apomonosi/sandboxing/cmd/agentctl@latest
$ agentctl config set provider=incus   # see docs/admin/providers/ for setup
$ agentctl create demo --image=images:ubuntu/24.04 --profile=default
$ agentctl start demo
$ agentctl shell demo
```

See [docs/user/quickstart.md](docs/user/quickstart.md) for the full walkthrough.

## Status

This is the first milestone: a real, working CLI against Incus on Linux, with
the provider-agnostic architecture (`Provider` interface, capability model,
profile schema) in place so Lima (macOS) and Hyper-V (Windows) backends can
be implemented next without reworking the core. See
[docs/admin/capability-matrix.md](docs/admin/capability-matrix.md) for what's
supported today.

## Development

```console
$ make build   # bin/agentctl
$ make test    # unit tests, no Incus/Lima/Hyper-V required
$ make test-integration   # requires a real, initialized Incus daemon
$ make lint
```
