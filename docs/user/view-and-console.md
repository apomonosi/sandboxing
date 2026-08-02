# Viewing a Sandbox

`agentctl view <name>` opens a viewer against the sandbox's own display —
useful when an agent drives a GUI (browser automation, computer-use style
workflows) and you want to watch or interact with it.

## Why not X11 forwarding?

Because an X server has **no isolation between clients**. Any X client
connected to a display can, in general, read the keystrokes and screen
contents of every other client on that same display — there's no per-client
sandboxing in the X protocol itself. Forwarding a compromised agent's X
session onto a shared display (yours, or worse, a shared server's) hands it
exactly the kind of cross-boundary access this whole tool exists to prevent.

So `agentctl view` never does raw X11 forwarding, on any provider. Instead:

| Provider | Mechanism |
|---|---|
| Incus | Native SPICE console (`incus console --type=vga`), talking directly to the VM's own virtual display |
| Hyper-V | Native VMConnect / Enhanced Session Mode — the most mature console story of the three backends |
| Lima | The one remaining gap on an otherwise fully-implemented backend: no first-class GUI console exists yet upstream, so agentctl plans a dedicated VNC bridge against the VM's own display (not X11 forwarding) — see the capability matrix, currently `UnderDevelopment` |

If you need to run an X application inside the sandbox for a legacy reason,
run it against a **nested, isolated display inside the guest** (e.g. a Wayland
compositor or a jailed Xephyr instance confined to that one VM) and view the
result via the mechanisms above — never expose the guest's X socket to the
host or to any other client.

## Read-only viewing

Pass `--read-only` if the provider supports it, to watch without being able
to interact — useful when you want visibility without the ability to
accidentally (or maliciously, via a second party) drive the agent's session.
