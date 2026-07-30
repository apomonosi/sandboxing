# Hyper-V Setup (Windows)

!!! note
    The Hyper-V backend is a capability-table-driven **stub** this
    milestone — every lifecycle command currently responds
    `UnderDevelopment`. This page describes the target setup and current
    gaps so Windows users know what to expect; see the
    [capability matrix](../capability-matrix.md) for the full picture.

## Enable Hyper-V (once the backend lands)

Hyper-V requires Windows 11/10 Pro, Enterprise, or Education (not Home) with
virtualization enabled in firmware:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All
```

Reboot when prompted.

## PowerShell execution policy

`agentctl`'s Hyper-V backend will drive Hyper-V's own PowerShell cmdlets
(`New-VM`, `Start-VM`, `Add-VMNetworkAdapterExtendedAcl`, ...). Ensure your
execution policy allows running the scripts agentctl invokes; consult your
organization's PowerShell policy before changing this on a managed machine.

## Known gap versus Incus/Lima

| Gap | Detail |
|---|---|
| No port-publish primitive | Hyper-V has no built-in "publish a port" concept. `agentctl` will report `ManualWorkaround` for `--port` and print a concrete NAT-switch + `netsh interface portproxy` procedure until this is automated. |

Everything else — lifecycle, network ACLs (Extended Port ACLs), snapshots
(Standard/Production checkpoints), and console (VMConnect/Enhanced Session
Mode) — has a strong, mature native primitive; the current `UnderDevelopment`
status on those reflects agentctl's own wiring being incomplete, not a
platform limitation.

## What agentctl will assume exists (once implemented)

- Hyper-V enabled and a default virtual switch (or a NAT switch, for port
  publishing) configured.
- PowerShell reachable non-interactively for the commands agentctl issues.
