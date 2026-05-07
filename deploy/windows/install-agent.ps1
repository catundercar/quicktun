<#
.SYNOPSIS
Installs quicktun-agent as a Windows service on a bastion host.

.DESCRIPTION
Copies the agent binary to C:\Program Files\quicktun\, writes a config to
C:\ProgramData\quicktun\agent.yaml, and registers a Windows service named
"quicktun-agent" via NSSM.

.NOTES
Requires:
  - PowerShell 5.1+ (built into Windows 10/11/Server 2016+)
  - Administrator privileges
  - NSSM downloaded from https://nssm.cc/download (place nssm.exe alongside
    this script OR in PATH)
  - rathole.exe at C:\Program Files\quicktun\rathole.exe (download from
    https://github.com/rapiz1/rathole/releases)

.PARAMETER Token
Raw site agent token from `quicktun site get-install-command`.

.PARAMETER ControlEndpoint
Control plane gRPC address, e.g. control.example.com:443.

.PARAMETER AuthProxy
Auth-proxy public address, e.g. relay.example.com:443.

.PARAMETER TLSInsecure
Skip TLS verification (dev only).

.EXAMPLE
.\install-agent.ps1 -Token "abc123..." -ControlEndpoint "control.example.com:443"
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)]
    [string]$Token,
    [Parameter(Mandatory=$true)]
    [string]$ControlEndpoint,
    [string]$AuthProxy = "",
    [switch]$TLSInsecure
)

$ErrorActionPreference = "Stop"

# Require admin
$current = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $current.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "This script must run as Administrator."
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$BinDir    = "C:\Program Files\quicktun"
$DataDir   = "C:\ProgramData\quicktun"
$LogDir    = "C:\ProgramData\quicktun\logs"

# Find nssm.exe
$Nssm = Join-Path $ScriptDir "nssm.exe"
if (-not (Test-Path $Nssm)) {
    $Nssm = (Get-Command nssm.exe -ErrorAction SilentlyContinue).Path
}
if (-not $Nssm) {
    throw "nssm.exe not found. Download from https://nssm.cc/download and place beside this script."
}

# Find agent binary
$AgentBin = Join-Path $ScriptDir "..\..\bin\quicktun-agent.exe"
if (-not (Test-Path $AgentBin)) {
    throw "quicktun-agent.exe not found at $AgentBin. Build with 'GOOS=windows GOARCH=amd64 go build -o bin/quicktun-agent.exe ./cmd/quicktun-agent'."
}

Write-Host "==> Creating directories"
New-Item -ItemType Directory -Force -Path $BinDir, $DataDir, $LogDir | Out-Null

Write-Host "==> Installing binary"
Copy-Item -Path $AgentBin -Destination (Join-Path $BinDir "quicktun-agent.exe") -Force

Write-Host "==> Writing config"
$ConfigPath = Join-Path $DataDir "agent.yaml"
$TLSValue = if ($TLSInsecure) { "true" } else { "false" }
$ConfigContent = @"
control_endpoint: $ControlEndpoint
token: $Token
state_dir: C:\ProgramData\quicktun\agent-state
rathole_binary: C:\Program Files\quicktun\rathole.exe
rathole_args:
  - --client
tls_insecure: $TLSValue
"@
if ($AuthProxy -ne "") {
    $ConfigContent += "`n# auth_proxy_endpoint received via Bootstrap; uncomment to override:`n# auth_proxy_endpoint: $AuthProxy`n"
}
Set-Content -Path $ConfigPath -Value $ConfigContent -Encoding UTF8

# Tighten ACL on config (only Administrators + SYSTEM)
$Acl = Get-Acl $ConfigPath
$Acl.SetAccessRuleProtection($true, $false)
$Acl.Access | ForEach-Object { $Acl.RemoveAccessRule($_) | Out-Null }
$AdminRule = New-Object System.Security.AccessControl.FileSystemAccessRule("Administrators","FullControl","Allow")
$SystemRule = New-Object System.Security.AccessControl.FileSystemAccessRule("SYSTEM","FullControl","Allow")
$Acl.AddAccessRule($AdminRule); $Acl.AddAccessRule($SystemRule)
Set-Acl -Path $ConfigPath -AclObject $Acl

Write-Host "==> Registering Windows service via NSSM"
$ServiceName = "quicktun-agent"
& $Nssm stop $ServiceName 2>$null | Out-Null
& $Nssm remove $ServiceName confirm 2>$null | Out-Null
& $Nssm install $ServiceName (Join-Path $BinDir "quicktun-agent.exe") `
    "run" "--config" $ConfigPath
& $Nssm set $ServiceName AppStdout (Join-Path $LogDir "agent.log")
& $Nssm set $ServiceName AppStderr (Join-Path $LogDir "agent.log")
& $Nssm set $ServiceName AppRotateFiles 1
& $Nssm set $ServiceName AppRotateBytes 10485760  # 10MB
& $Nssm set $ServiceName Start SERVICE_AUTO_START
& $Nssm set $ServiceName Description "quicktun site agent - tunnels local services to the quicktun control plane"
& $Nssm start $ServiceName

Write-Host ""
Write-Host "Done. Service status:"
Get-Service quicktun-agent | Format-Table -AutoSize
Write-Host ""
Write-Host "Logs: $LogDir\agent.log"
Write-Host "Stop:    nssm stop quicktun-agent"
Write-Host "Remove:  nssm remove quicktun-agent confirm"
