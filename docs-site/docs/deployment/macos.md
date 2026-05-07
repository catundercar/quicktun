---
sidebar_position: 2
---

# macOS 部署(agent)

Phase 1 仅支持 macOS 作为 **agent**(跳板机)。控制面只跑 Linux。

agent 用 **LaunchDaemon**(系统级,boot 时自启),不用 LaunchAgent(per-user)。

## 简化:phase 1 daemon 跑在 root

正式 macOS 系统服务应该跑在专用 unprivileged 用户下(`dscl . -create /Users/_quicktun-agent`),Phase 1 偷懒**直接跑 root**:

- 实施代价:加一个 `dscl` 包装函数,处理 UID 分配、home directory placeholder...
- 风险评估:agent 文件系统占用小,token 文件 `chmod 0600`;LAN 内的客户端机器不直接从公网可达
- 妥协:Phase 1 文档里**显式标注**,Phase 2 按计划补 unprivileged user

如果你觉得 root 不可接受,自己 fork install-agent.sh 改成 `dscl create user`(欢迎 PR)。

## 步骤

### 1. 装 rathole

```bash
RATHOLE_VERSION=v0.5.0

# Apple Silicon (M1/M2/M3)
curl -L "https://github.com/rapiz1/rathole/releases/download/${RATHOLE_VERSION}/rathole-aarch64-apple-darwin.zip" \
    -o /tmp/rathole.zip

# Intel Mac (older)
# curl -L ".../rathole-x86_64-apple-darwin.zip" -o /tmp/rathole.zip

unzip /tmp/rathole.zip -d /tmp/rathole
sudo install -m 0755 /tmp/rathole/rathole /usr/local/bin/rathole
rathole --version
```

### 2. 编译 agent

```bash
git clone https://github.com/catundercar/quicktun.git
cd quicktun
make build
ls bin/quicktun-agent  # 应该存在
```

### 3. 跑 install-agent.sh

```bash
sudo ./deploy/install-agent.sh \
    --token <RAW_TOKEN> \
    --control-endpoint control.example.com:443 \
    --auth-proxy relay.example.com:443
```

脚本检测到 Darwin 后:

1. 创建 `/etc/quicktun`、`/var/lib/quicktun-agent`、`/var/log/quicktun-agent`(mode 0755)
2. 拷贝 `bin/quicktun-agent` 到 `/usr/local/bin/`
3. 装 plist 到 `/Library/LaunchDaemons/com.tulip.quicktun-agent.plist`
4. 写 `/etc/quicktun/agent.yaml`(0600)
5. `launchctl unload`(防重载)+ `launchctl load -w` 启动

### 4. 验证

```bash
sudo launchctl list | grep quicktun-agent
# 看 PID 列有数字 = 进程跑起来了

sudo tail -f /var/log/quicktun-agent/agent.log
```

## plist 文件

`deploy/launchd/com.tulip.quicktun-agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tulip.quicktun-agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/quicktun-agent</string>
        <string>run</string>
        <string>--config</string>
        <string>/etc/quicktun/agent.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>/var/log/quicktun-agent/agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/quicktun-agent/agent.log</string>
    <key>ThrottleInterval</key>
    <integer>10</integer>
</dict>
</plist>
```

要点:

- `RunAtLoad=true`:启动时立即运行
- `KeepAlive { SuccessfulExit: false }`:进程退出且 exit code != 0 时由 launchd 重启;exit 0 不重启(允许优雅停机)
- `ThrottleInterval=10`:崩溃太频繁时 launchd 至少等 10 秒再重启
- 没设 `UserName`,默认 root

## 操作命令

| 操作 | 命令 |
|---|---|
| 查看是否在跑 | `sudo launchctl list \| grep quicktun-agent` |
| 立即启动 | `sudo launchctl load -w /Library/LaunchDaemons/com.tulip.quicktun-agent.plist` |
| 立即停止 | `sudo launchctl unload /Library/LaunchDaemons/com.tulip.quicktun-agent.plist` |
| 永久禁用(boot 不起) | `sudo launchctl unload -w ...`(写 Disabled 键) |
| Tail 日志 | `sudo tail -f /var/log/quicktun-agent/agent.log` |

## 升级

```bash
cd quicktun
git pull && make build
sudo ./deploy/install-agent.sh \
    --token <SAME_TOKEN> \
    --control-endpoint <SAME_HOST>
```

脚本会先 `unload`(忽略 "not loaded" 错误),再 install + `load -w`。

## 卸载

```bash
sudo launchctl unload -w /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
sudo rm /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
sudo rm /usr/local/bin/quicktun-agent
sudo rm -rf /etc/quicktun /var/lib/quicktun-agent /var/log/quicktun-agent
```

## 已知限制

- daemon 跑在 **root**(Phase 1)。Phase 2 用 `_quicktun-agent` 系统用户。
- 没有 macOS 控制面支持(Phase 1 控制面仅 Linux)
- `/healthz` 在 macOS 默认禁用,与 Linux 一致;手动开:`agent.yaml` 加 `health_listen_addr: 127.0.0.1:8445`
