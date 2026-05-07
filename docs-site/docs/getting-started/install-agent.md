---
sidebar_position: 3
---

# 安装 agent

每台跳板机装一个 `quicktun-agent`。Linux 走 systemd,macOS 走 launchd,Windows 走 NSSM。本页以 **Linux** 为主线;另外两个平台见 [部署 / macOS](../deployment/macos.md) 和 [部署 / Windows](../deployment/windows.md)。

## 前置条件

- 跳板机:Linux x86_64 + sudo
- 跳板机能出站连接控制面 `:443`
- 控制面已部署完成且 admin 已用 CLI 创建好 `project / site / service`
- admin 已通过 `quicktun site get-install-command` 拿到一个 **raw token**

## 准备 raw token

在 operator 工作站上:

```bash
quicktun project create clinic-net \
    --display-name "Clinic Network" \
    --port-range 20000-20099

quicktun site create clinic-net bastion-1 \
    --display-name "Bastion 1"

quicktun service create clinic-net/bastion-1 ssh \
    --target-addr 127.0.0.1 \
    --target-port 22 \
    --proto tcp

# 拿 token(只展示一次,妥善保存)
quicktun site get-install-command clinic-net/bastion-1
```

输出大约长这样:

```
Run on the bastion host:

  sudo ./deploy/install-agent.sh \
      --token a1b2c3d4...64hex \
      --control-endpoint control.example.com:443 \
      --auth-proxy relay.example.com:443
```

复制下来,token 只输出一次。

## 跑 install-agent.sh

在跳板机上:

```bash
git clone https://github.com/catundercar/quicktun.git
cd quicktun
make build

sudo ./deploy/install-agent.sh \
    --token a1b2c3d4... \
    --control-endpoint control.example.com:443 \
    --auth-proxy relay.example.com:443
```

脚本根据 `uname -s` 自动选 Linux 或 Darwin 分支:

- **Linux**:创建 `quicktun-agent` 用户、装 systemd unit、写 `/etc/quicktun/agent.yaml`(0600,owner `quicktun-agent`)、`systemctl enable --now`
- **Darwin**(macOS):装 LaunchDaemon 到 `/Library/LaunchDaemons/`、`launchctl load -w` 启动

脚本是**幂等**的,升级时直接重跑(会先 `unload` / `restart`)。

## 装 rathole-client

agent 不会自己下载 rathole,你需要手动装:

```bash
RATHOLE_VERSION=v0.5.0
curl -L "https://github.com/rapiz1/rathole/releases/download/${RATHOLE_VERSION}/rathole-x86_64-unknown-linux-gnu.zip" \
    -o /tmp/rathole.zip
unzip /tmp/rathole.zip -d /tmp/rathole
sudo install -m 0755 /tmp/rathole/rathole /usr/local/bin/rathole
```

`agent.yaml` 默认配 `rathole_binary: /usr/local/bin/rathole`,所以路径只要对上就行。

## 验证

```bash
sudo systemctl status quicktun-agent
sudo journalctl -u quicktun-agent -f
```

正常的话,~1 秒内你会看到:

- `agent: bootstrap ok`
- `agent: rendered rathole-client.toml`
- `supervisor: rathole started, pid=...`

回到 operator 工作站:

```bash
quicktun site get clinic-net/bastion-1
# last_seen_time 应该是几秒前的时间
```

## 测试转发

```bash
# 工作站
quicktun forward clinic-net/bastion-1/ssh --local-port 2222

# 另一个终端
ssh -p 2222 user@127.0.0.1
# (用 site 自己的 SSH key / 密码)
```

成功 SSH 进去 = 端到端打通。

## 跨平台

- [macOS 详细步骤](../deployment/macos.md)
- [Windows 详细步骤](../deployment/windows.md)
