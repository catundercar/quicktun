[CmdletBinding()]
param([switch]$Purge)

$ErrorActionPreference = "Stop"
$current = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $current.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run as Administrator."
}

$Nssm = (Get-Command nssm.exe -ErrorAction SilentlyContinue).Path
if (-not $Nssm) { throw "nssm.exe not on PATH" }

& $Nssm stop quicktun-agent 2>$null | Out-Null
& $Nssm remove quicktun-agent confirm 2>$null | Out-Null

if ($Purge) {
    Remove-Item -Recurse -Force "C:\ProgramData\quicktun" -ErrorAction SilentlyContinue
    Remove-Item -Recurse -Force "C:\Program Files\quicktun" -ErrorAction SilentlyContinue
    Write-Host "Removed config + binary."
}
Write-Host "quicktun-agent service removed."
