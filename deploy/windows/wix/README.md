# Building the quicktun-agent MSI

The MSI installer wraps `quicktun-agent.exe` as a native Windows service using
WiX 5. The service calls the `service-run` subcommand, which integrates with
the Windows Service Control Manager (SCM) via `golang.org/x/sys/windows/svc`.
No NSSM dependency is required when using the MSI path.

## Build locally (on Windows)

Requirements:

- Windows 10/11 or Windows Server 2019/2022
- .NET SDK 8 or later (`dotnet --version`)
- WiX 5:
  ```powershell
  dotnet tool install --global wix
  ```
- A Windows build of `quicktun-agent.exe`:
  ```powershell
  $env:CGO_ENABLED = "0"
  go build -o quicktun-agent.exe -ldflags "-s -w" .\cmd\quicktun-agent
  ```

Build the MSI:

```powershell
$exePath   = (Resolve-Path quicktun-agent.exe).Path
$yamlPath  = (Resolve-Path deploy\windows\agent.yaml.example).Path

wix build deploy\windows\wix\Product.wxs `
    -define AgentExePath="$exePath" `
    -define AgentYamlExamplePath="$yamlPath" `
    -out quicktun-agent.msi
```

## Build via GitHub Actions

Push a tag matching `v*` (e.g. `v0.1.0`) — the `build-windows-msi.yml`
workflow builds on a `windows-latest` runner and:

1. Attaches `quicktun-agent.msi` to the GitHub Release.
2. Also uploads it as a workflow artifact (30-day retention) for `workflow_dispatch` runs.

## Install the MSI on a target machine

Double-click `quicktun-agent.msi`, or from an elevated PowerShell:

```powershell
msiexec /i quicktun-agent.msi /qn
```

After install, the service is registered but starts **only after you configure
it**. Edit the config file:

```powershell
Copy-Item "C:\ProgramData\quicktun\agent.yaml.example" `
          "C:\ProgramData\quicktun\agent.yaml"
notepad "C:\ProgramData\quicktun\agent.yaml"
```

Fill in `control_endpoint` and `token`, then start the service:

```powershell
Start-Service quicktun-agent
Get-Service  quicktun-agent
```

Logs go to `C:\ProgramData\quicktun\logs\agent.log`.

## Uninstall

```powershell
msiexec /x quicktun-agent.msi /qn
```

The service is stopped and removed automatically.

## UpgradeCode GUID

The `UpgradeCode` in `Product.wxs` is:

```
6F2A8E1C-3D4B-4F57-9A0C-2B8E5F1A7C3D
```

**This GUID must not change between releases.** It is the stable identity
Windows uses to recognise that an MSI being installed is an upgrade of the same
product (not a new one). Changing it would cause upgrades to leave old
installations behind.
