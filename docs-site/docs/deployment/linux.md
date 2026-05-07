---
sidebar_position: 1
---

# Linux 部署

Phase 1 的目标平台。控制面只支持 Linux,agent 主要也是 Linux。

## 控制面

### 目录布局

```
/etc/quicktun/                  ← config (owner: quicktun)
├── server.yaml                 0640
└── authproxy.yaml              0640

/var/lib/quicktun/              ← state (owner: quicktun, mode 0750)
├── quicktun.db                 SQLite,所有状态
└── relays/                     rathole-server-<project>.toml 文件

/var/log/quicktun/              ← log (owner: quicktun, mode 0750)
                                 (logs 默认走 stderr → journald)

/usr/local/bin/                 ← binaries
├── quicktun-server
├── quicktun-authproxy
└── rathole                     (operator 自己装)
```

### systemd units

`deploy/install-server.sh` 装好 3 个 unit:

#### `quicktun-server.service`

```ini
[Unit]
Description=quicktun control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=quicktun
Group=quicktun
ExecStartPre=/usr/local/bin/quicktun-server migrate --config /etc/quicktun/server.yaml
ExecStart=/usr/local/bin/quicktun-server serve --config /etc/quicktun/server.yaml
Restart=on-failure
RestartSec=5s
StartLimitBurst=10
StartLimitIntervalSec=60s

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/quicktun /var/log/quicktun

[Install]
WantedBy=multi-user.target
```

要点:

- `ExecStartPre` 跑 `migrate`,schema 升级在 server start 前自动完成
- `ProtectSystem=strict + ReadWritePaths=...`:文件系统大部分只读,只允许写指定目录
- `CapabilityBoundingSet=` + `AmbientCapabilities=`:无任何 capability(普通 user 模式)

#### `quicktun-authproxy.service`

```ini
[Unit]
Requires=quicktun-server.service
After=quicktun-server.service

[Service]
ExecStart=/usr/local/bin/quicktun-authproxy run --config /etc/quicktun/authproxy.yaml
ReadOnlyPaths=/var/lib/quicktun
```

`ReadOnlyPaths`:auth-proxy 不写 DB,只读访问,符合 [auth-proxy 设计](../architecture/auth-proxy.md) 中的描述。

### install-server.sh

源码见 `deploy/install-server.sh`,详细步骤见 [快速开始 / 安装控制面](../getting-started/install-server.md)。

幂等关键点:

- `ensure_user` / `ensure_dir`:只在不存在时创建
- 配置文件:已存在时不覆盖(`ensure_file_unchanged_or_prompt`)
- admin operator:已有则不再 prompt
- `systemctl enable --now`:幂等

升级流程:

```bash
git pull && make build
sudo ./deploy/install-server.sh
sudo systemctl restart quicktun-server quicktun-authproxy
```

## Agent

### 目录布局

```
/etc/quicktun/agent.yaml        0600 owner: quicktun-agent
/var/lib/quicktun-agent/        0750 owner: quicktun-agent
└── rathole-client.toml         agent 渲染,reload 时重写
/var/log/quicktun-agent/        0750 (logs 走 journald)
/usr/local/bin/quicktun-agent
/usr/local/bin/rathole          (operator 自己装)
```

### systemd unit

```ini
[Unit]
Description=quicktun site agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=quicktun-agent
Group=quicktun-agent
ExecStart=/usr/local/bin/quicktun-agent run --config /etc/quicktun/agent.yaml
Restart=on-failure
RestartSec=5s

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/quicktun-agent /var/log/quicktun-agent

[Install]
WantedBy=multi-user.target
```

注意:`User=quicktun-agent`,token 配文件 0600,只有这个 user 能读。

### install-agent.sh

调用方式:

```bash
sudo ./deploy/install-agent.sh \
    --token <RAW_TOKEN> \
    --control-endpoint control.example.com:443 \
    [--auth-proxy relay.example.com:443] \
    [--tls-insecure]
```

脚本根据 `uname -s` 分发到 Linux / Darwin。Linux 流程:

1. 创建 `quicktun-agent` 系统用户(如不存在)
2. 创建 `/etc/quicktun`、`/var/lib/quicktun-agent`、`/var/log/quicktun-agent`
3. 拷贝 `bin/quicktun-agent` 到 `/usr/local/bin/`
4. 装 systemd unit
5. 写 `/etc/quicktun/agent.yaml`(0600)
6. `systemctl enable --now quicktun-agent.service`

升级:

```bash
git pull && make build
sudo ./deploy/install-agent.sh --token <SAME_TOKEN> --control-endpoint <SAME_HOST>
sudo systemctl restart quicktun-agent
```

幂等:重跑只刷新二进制和 unit,**不会**重写 agent.yaml(避免覆盖 operator 的手工编辑)。

## 操作命令

| 操作 | 命令 |
|---|---|
| 启动 / 停止 / 重启 | `systemctl {start,stop,restart} quicktun-server quicktun-authproxy quicktun-agent` |
| 查看状态 | `systemctl status quicktun-server` |
| Tail 日志 | `journalctl -u quicktun-server -f` |
| 查看启动失败原因 | `systemctl status quicktun-server -l` |
| 查 systemd 配置生效情况 | `systemctl cat quicktun-server` |
| Reload daemon(改了 unit 文件后) | `sudo systemctl daemon-reload` |

## 备份

SQLite 文件 `/var/lib/quicktun/quicktun.db` 是**唯一的状态**(operators / projects / sites / services / sessions / agent token hashes 都在这)。

```bash
sqlite3 /var/lib/quicktun/quicktun.db ".backup '/path/out.db'"
```

`.backup` 是 online-safe 的,**不要直接 `cp`** 一个正在被 server 用的 DB。

恢复:停 service → 替换文件 → 起 service。

## 卸载

```bash
sudo systemctl disable --now quicktun-server quicktun-authproxy quicktun-agent
sudo rm /etc/systemd/system/quicktun-{server,authproxy,agent}.service
sudo rm /usr/local/bin/quicktun-{server,authproxy,agent}
sudo userdel quicktun
sudo userdel quicktun-agent
# 谨慎删数据
sudo rm -rf /etc/quicktun /var/lib/quicktun /var/lib/quicktun-agent
```
