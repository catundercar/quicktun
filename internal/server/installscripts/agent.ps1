#Requires -RunAsAdministrator
<#
.SYNOPSIS
quicktun-agent quick installer (iwr-iex safe).

.DESCRIPTION
Designed for one-line install:
  $env:QT_TOKEN="..."; $env:QT_ENDPOINT="host:port"; `
    iwr -useb http://control.example.com/install/agent.ps1 | iex

Reads from environment variables (no parameter prompts):
  $env:QT_TOKEN         - site agent raw token (required)
  $env:QT_ENDPOINT      - control-plane gRPC host:port (required)
  $env:QT_AGENT_URL     - quicktun-agent.exe download URL (optional)
  $env:QT_TLS_INSECURE  - "true" to skip TLS verification (default false)

Registers the agent as a Windows service via native sc.exe (no NSSM).
The service runs in foreground SCM-aware mode using the agent's
`service-run` subcommand (golang.org/x/sys/windows/svc integrated).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

# Read env vars
$Token = $env:QT_TOKEN
$Endpoint = $env:QT_ENDPOINT
$AgentUrl = $env:QT_AGENT_URL
$TlsInsecure = if ($env:QT_TLS_INSECURE -eq "true") { "true" } else { "false" }

if (-not $Token)    { throw "QT_TOKEN env var required" }
if (-not $Endpoint) { throw "QT_ENDPOINT env var required" }

if (-not $AgentUrl) {
    # Default to GitHub release. Operator can override via QT_AGENT_URL.
    $AgentUrl = "https://github.com/catundercar/quicktun/releases/latest/download/quicktun-agent.exe"
}

$ServiceName  = "quicktun-agent"
$InstallDir   = "C:\Program Files\quicktun"
$DataDir      = "C:\ProgramData\quicktun"
$LogsDir      = "C:\ProgramData\quicktun\logs"
$AgentExe     = Join-Path $InstallDir "quicktun-agent.exe"
$ConfigPath   = Join-Path $DataDir "agent.yaml"

Write-Host "==> creating directories"
New-Item -ItemType Directory -Force -Path $InstallDir, $DataDir, $LogsDir | Out-Null

# Stop + remove existing service before replacing binary.
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Status -eq "Running") {
        Write-Host "==> stopping existing service"
        Stop-Service -Name $ServiceName -Force
        # Wait briefly for stop
        for ($i = 0; $i -lt 20; $i++) {
            $svc = Get-Service -Name $ServiceName
            if ($svc.Status -eq "Stopped") { break }
            Start-Sleep -Milliseconds 200
        }
    }
    Write-Host "==> removing existing service"
    sc.exe delete $ServiceName | Out-Null
    Start-Sleep -Seconds 1
}

# Download (or refresh) the agent binary.
if (-not (Test-Path $AgentExe)) {
    Write-Host "==> fetching quicktun-agent.exe from $AgentUrl"
    try {
        Invoke-WebRequest -UseBasicParsing -Uri $AgentUrl -OutFile "$AgentExe.new"
        Move-Item -Force "$AgentExe.new" $AgentExe
    } catch {
        throw "failed to download agent: $_. Place quicktun-agent.exe at $AgentExe manually then rerun."
    }
} else {
    Write-Host "==> reusing existing $AgentExe"
}

# Write config
Write-Host "==> writing $ConfigPath"
$ConfigContent = @"
control_endpoint: $Endpoint
token: $Token
state_dir: $DataDir\agent-state
rathole_binary: $InstallDir\rathole.exe
rathole_args:
  - --client
tls_insecure: $TlsInsecure
"@
Set-Content -Path $ConfigPath -Value $ConfigContent -Encoding UTF8

# Restrict ACL: Administrators + SYSTEM only.
$Acl = Get-Acl $ConfigPath
$Acl.SetAccessRuleProtection($true, $false)
$Acl.Access | ForEach-Object { $Acl.RemoveAccessRule($_) | Out-Null }
$AdminRule  = New-Object System.Security.AccessControl.FileSystemAccessRule("Administrators","FullControl","Allow")
$SystemRule = New-Object System.Security.AccessControl.FileSystemAccessRule("SYSTEM","FullControl","Allow")
$Acl.AddAccessRule($AdminRule)
$Acl.AddAccessRule($SystemRule)
Set-Acl -Path $ConfigPath -AclObject $Acl

# Register Windows service via native sc.exe. The agent's `service-run`
# subcommand uses golang.org/x/sys/windows/svc, so it integrates with SCM
# (responds to Stop / Shutdown / Interrogate).
$BinPath = '"' + $AgentExe + '" service-run --config "' + $ConfigPath + '"'
Write-Host "==> registering service: $ServiceName"
sc.exe create $ServiceName binPath= "$BinPath" start= auto DisplayName= "quicktun site agent" | Out-Null
sc.exe description $ServiceName "Tunnels local services to the quicktun control plane" | Out-Null
sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null

Write-Host "==> starting service"
sc.exe start $ServiceName | Out-Null

# Wait for service to enter Running state (or fail explicitly).
$started = $false
for ($i = 0; $i -lt 30; $i++) {
    Start-Sleep -Milliseconds 300
    $svc = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -eq "Running") { $started = $true; break }
}
if (-not $started) {
    Write-Warning "service did not reach Running state in 9s; check Event Log + $LogsDir"
}

Write-Host ""
Write-Host "Done. Service status:"
Get-Service -Name $ServiceName | Format-Table -AutoSize
Write-Host "Stop:    sc.exe stop $ServiceName"
Write-Host "Remove:  sc.exe stop $ServiceName; sc.exe delete $ServiceName"
Write-Host "Logs:    Windows Event Log + agent's zap output (currently stderr, captured by SCM)"
Write-Host "Note:    install rathole.exe at $InstallDir\rathole.exe from https://github.com/rapiz1/rathole/releases"
