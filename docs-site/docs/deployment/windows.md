---
sidebar_position: 3
---

# Windows 部署(agent)

Phase 1 通过 [NSSM](https://nssm.cc/download)(Non-Sucking Service Manager)把 `quicktun-agent.exe` 包装成 Windows 服务。已测试 Windows 10 / 11 / Server 2019 / 2022。

## 前置条件

- PowerShell 5.1+(Win10/11 自带)
- 管理员账号
- `nssm.exe` 放在脚本同目录或 PATH 中
- `rathole.exe` 放在 `C:\Program Files\quicktun\rathole.exe`(从 [rapiz1/rathole](https://github.com/rapiz1/rathole/releases) 下载)
- 已编译的 `quicktun-agent.exe`(从 Linux/macOS 交叉编译):
  ```bash
  GOOS=windows GOARCH=amd64 go build -o bin/quicktun-agent.exe ./cmd/quicktun-agent
  ```

## 装 NSSM

NSSM 用 portable exe 形态分发,扔进 `quicktun\deploy\windows\` 目录就行:

```powershell
# 在 deploy\windows\ 下
Invoke-WebRequest https://nssm.cc/release/nssm-2.24.zip -OutFile nssm.zip
Expand-Archive nssm.zip -DestinationPath .
copy nssm-2.24\win64\nssm.exe .
del nssm.zip
rmdir /s /q nssm-2.24
```

## 装 rathole.exe

```powershell
$RatholeVersion = "v0.5.0"
$Url = "https://github.com/rapiz1/rathole/releases/download/$RatholeVersion/rathole-x86_64-pc-windows-msvc.zip"
Invoke-WebRequest $Url -OutFile $env:TEMP\rathole.zip
Expand-Archive $env:TEMP\rathole.zip -DestinationPath $env:TEMP\rathole
New-Item -ItemType Directory -Force "C:\Program Files\quicktun" | Out-Null
copy $env:TEMP\rathole\rathole.exe "C:\Program Files\quicktun\rathole.exe"
```

## 跑 install-agent.ps1

以管理员身份打开 PowerShell:

```powershell
cd C:\path\to\quicktun
.\deploy\windows\install-agent.ps1 `
    -Token "<RAW_TOKEN>" `
    -ControlEndpoint "control.example.com:443"
```

可选:

```powershell
.\deploy\windows\install-agent.ps1 `
    -Token "..." `
    -ControlEndpoint "control.example.com:443" `
    -AuthProxy "relay.example.com:443" `
    -TLSInsecure   # 仅 dev
```

脚本会:

1. 创建目录:
   - `C:\Program Files\quicktun\`(binary)
   - `C:\ProgramData\quicktun\`(config + state)
   - `C:\ProgramData\quicktun\logs\`(NSSM 日志)
2. 拷贝 `quicktun-agent.exe` 到 `C:\Program Files\quicktun\`
3. 写 `C:\ProgramData\quicktun\agent.yaml`,**ACL 收紧**(只有 Administrators + SYSTEM 有访问权)
4. NSSM 注册服务 `quicktun-agent`,配 stdout/stderr → `agent.log`,日志轮换 10MB
5. 启动服务

## 验证

```powershell
Get-Service quicktun-agent
# Status   Name              DisplayName
# ------   ----              -----------
# Running  quicktun-agent    quicktun site agent...

Get-Content -Tail 20 -Wait C:\ProgramData\quicktun\logs\agent.log
```

## NSSM 配置详情

`install-agent.ps1` 设置的 NSSM 参数:

| NSSM key | Value | 说明 |
|---|---|---|
| `Application` | `C:\Program Files\quicktun\quicktun-agent.exe` | 主程序 |
| `AppParameters` | `run --config C:\ProgramData\quicktun\agent.yaml` | 启动参数 |
| `AppStdout` / `AppStderr` | `C:\ProgramData\quicktun\logs\agent.log` | stdout/stderr 重定向 |
| `AppRotateFiles` | `1` | 启用日志轮换 |
| `AppRotateBytes` | `10485760`(10 MB) | 单文件超过这个就轮换 |
| `Start` | `SERVICE_AUTO_START` | 系统启动时自动起 |

NSSM 服务跑在 `LocalSystem`(Phase 1 简化)。Phase 2 可改:

```powershell
nssm set quicktun-agent ObjectName <user> <password>
```

## 操作命令

```powershell
# 启停
Start-Service quicktun-agent
Stop-Service quicktun-agent
Restart-Service quicktun-agent

# 也可用 NSSM
nssm start quicktun-agent
nssm stop quicktun-agent

# 查日志
Get-Content -Tail 100 -Wait C:\ProgramData\quicktun\logs\agent.log
```

## 升级

```powershell
# 编译新二进制后,重跑 install-agent.ps1(脚本会先 stop + remove + reinstall)
.\deploy\windows\install-agent.ps1 -Token "..." -ControlEndpoint "..."
```

## 卸载

```powershell
.\deploy\windows\uninstall-agent.ps1            # 停 service + 卸载
.\deploy\windows\uninstall-agent.ps1 -Purge     # 也删 config + binary
```

或手动:

```powershell
nssm stop quicktun-agent
nssm remove quicktun-agent confirm
Remove-Item -Recurse -Force "C:\Program Files\quicktun"
Remove-Item -Recurse -Force "C:\ProgramData\quicktun"
```

## 已知限制

- 服务跑在 **LocalSystem**(Phase 1 简化)
- 没有原生的 `golang.org/x/sys/windows/svc` 集成,完全依赖 NSSM 包装(agent 是普通 console binary,NSSM 后台跑)
- 没有 Windows 控制面;Phase 1 控制面仅 Linux
