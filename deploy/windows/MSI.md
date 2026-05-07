# Windows MSI Installation

The `quicktun-agent.msi` installer is the recommended way to deploy the agent
on Windows. It registers `quicktun-agent` as a native Windows service — no
NSSM, no PowerShell scripts needed.

## Download

Get the latest `.msi` from the GitHub Releases page:

https://github.com/catundercar/quicktun/releases

## Install

Double-click the `.msi`, or from an elevated PowerShell:

```powershell
msiexec /i quicktun-agent.msi /qn
```

The installer:

1. Copies `quicktun-agent.exe` to `C:\Program Files\quicktun\`.
2. Creates `C:\ProgramData\quicktun\logs\` and `C:\ProgramData\quicktun\agent-state\`.
3. Drops `C:\ProgramData\quicktun\agent.yaml.example`.
4. Registers the `quicktun-agent` Windows service (auto-start, LocalSystem).

## Configure

Copy the example config and edit it:

```powershell
Copy-Item "C:\ProgramData\quicktun\agent.yaml.example" `
          "C:\ProgramData\quicktun\agent.yaml"
notepad "C:\ProgramData\quicktun\agent.yaml"
```

Fill in at minimum:

```yaml
control_endpoint: control.example.com:443
token: PASTE_FROM_INSTALL_COMMAND
state_dir: C:\ProgramData\quicktun\agent-state
rathole_binary: C:\Program Files\quicktun\rathole.exe
rathole_args:
  - --client
tls_insecure: false
```

Get your install token from:

```bash
quicktun site get-install-command my-team/bastion-1
```

## Start the service

```powershell
Start-Service quicktun-agent
Get-Service   quicktun-agent
```

Monitor logs:

```powershell
Get-Content -Tail 20 -Wait C:\ProgramData\quicktun\logs\agent.log
```

## Upgrade

Download the new `.msi` and run:

```powershell
msiexec /i quicktun-agent.msi /qn
```

The MSI automatically stops the old service, replaces the binary, and restarts
the service. The config file is not touched.

## Uninstall

```powershell
msiexec /x quicktun-agent.msi /qn
```

The service is stopped and removed. Config files and logs under
`C:\ProgramData\quicktun\` are left in place.

## Build from source

See [`deploy/windows/wix/README.md`](wix/README.md).

## When to use NSSM instead

The MSI is the recommended path for most Windows deployments.

NSSM (`deploy/windows/install-agent.ps1`) remains supported as a fallback for
environments where MSIs are blocked by policy (e.g., strict AppLocker
configurations), or where operators prefer a script-driven workflow.
