---
sidebar_position: 4
---

# quicktun service

管理 service(site 上暴露的端口)。

## 子命令

```
quicktun service list   <site>
quicktun service get    <name>
quicktun service create <site> <slug> --target-addr <addr> --target-port <port> [flags]
quicktun service delete <name> [--yes]
```

## list

```bash
quicktun service list clinic-net/bastion-1
quicktun service list clinic-net/bastion-1 --json
```

输出列:`NAME`、`DISPLAY`、`TARGET`、`PROTO`、`RELAY_PORT`。

`RELAY_PORT` 是控制面从 project 的 `port-range` 自动分配的端口。

## get

```bash
quicktun service get clinic-net/bastion-1/ssh
# 或完整名
quicktun service get projects/clinic-net/sites/bastion-1/services/ssh
```

## create

```bash
quicktun service create clinic-net/bastion-1 ssh \
    --target-addr 127.0.0.1 \
    --target-port 22 \
    --proto tcp \
    --display-name "SSH"
```

| Flag | 必填 | 说明 |
|---|---|---|
| `--target-addr` | ✅ | 跳板机能访问的目标地址。可以是 `127.0.0.1`(跳板机自己)或 LAN 内任意 IP `192.168.x.y` |
| `--target-port` | ✅ | 目标端口 |
| `--proto` | | `tcp`(默认)或 `udp`。Phase 1 实测主要 TCP |
| `--display-name` | | 给人看的名字 |
| `--json` | | 输出 JSON |

创建成功后:

- 控制面分配 `relay_port`(从 project 的 `port-range` 取一个空闲端口)
- 触发该 project 的 rathole-server 配置 reload(SIGHUP / 进程重启,看 `internal/relay/render.go`)
- 下次 agent heartbeat 拿到新的 `config_version`,重 bootstrap → 重 render rathole-client.toml → rathole-client 重启

简单说:**operator 创建 service 后 ~30 秒内,新端口就能 forward 了**。

## delete

```bash
quicktun service delete clinic-net/bastion-1/ssh
quicktun service delete clinic-net/bastion-1/ssh --yes
```

删 service 会:

1. 释放对应的 `relay_port`
2. 重 render rathole-server 配置
3. agent 下次 heartbeat 拿到新版本,重 bootstrap 摘掉这个 tunnel

## 例子:暴露 LAN 内多个机器

```bash
# 跳板机自己的 SSH
quicktun service create clinic-net/bastion-1 bastion-ssh \
    --target-addr 127.0.0.1 --target-port 22 --proto tcp

# LAN 内 PACS 服务器 RDP
quicktun service create clinic-net/bastion-1 pacs-rdp \
    --target-addr 192.168.10.50 --target-port 3389 --proto tcp

# LAN 内数据库
quicktun service create clinic-net/bastion-1 db \
    --target-addr 192.168.10.20 --target-port 5432 --proto tcp
```

每个 service 单独 forward,`quicktun forward clinic-net/bastion-1/pacs-rdp -p 3389` → `mstsc /v:127.0.0.1`。

## 资源命名

CLI 接受四段简写 `<project>/<site>/<service>` 和完整 6 段两种形态。

## 看源码

`cmd/quicktun/cmd_service.go`
