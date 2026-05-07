---
sidebar_position: 5
---

# quicktun forward

把一个 service 的 relay 端口映射到本地 TCP 端口,通过 auth-proxy 鉴权。

## 用法

```
quicktun forward <service-name> --local-port <port> [--bind <addr>]
```

## Flags

| Flag | 默认 | 说明 |
|---|---|---|
| `--local-port` / `-p` | `0`(随机) | 本地监听端口 |
| `--bind` | `127.0.0.1` | 本地监听地址。**默认只绑 loopback**,防止把内网服务暴露到本地子网 |

## 示例

### SSH

```bash
quicktun forward clinic-net/bastion-1/ssh --local-port 2222
# Forwarding 127.0.0.1:2222 -> projects/clinic-net/sites/bastion-1/services/ssh (auth-proxy: relay.example.com:443)

# 另一个终端
ssh -p 2222 user@127.0.0.1
```

### RDP

```bash
quicktun forward clinic-net/bastion-1/pacs-rdp -p 3389
# 然后 Microsoft Remote Desktop 连 127.0.0.1:3389
```

### 数据库

```bash
quicktun forward clinic-net/bastion-1/postgres -p 5432
# psql -h 127.0.0.1 -p 5432 -U app -d clinical_db
```

### 多客户端共享一个 listener

每个 accept 的本地连接独立开一个 CONNECT tunnel。所以你可以多个 ssh / mosh / rsync 并发跑在同一个 `forward` 上:

```bash
# 终端 A
quicktun forward clinic-net/bastion-1/ssh -p 2222

# 终端 B,C
ssh -p 2222 user@127.0.0.1
ssh -p 2222 user@127.0.0.1
rsync -e "ssh -p 2222" ... user@127.0.0.1:/path
```

## 工作原理

```
ssh -p 2222 ──► 127.0.0.1:2222 (本地)
                       │
                       │ accept
                       ▼
              ┌─────────────────┐
              │ quicktun        │
              │ forward         │
              └────────┬────────┘
                       │
                       │ TCP dial → relay.example.com:443
                       │ CONNECT 127.0.0.1:<svc_relay_port> HTTP/1.1
                       │ Authorization: Bearer <session_token>
                       ▼
              ┌─────────────────┐
              │ nginx :443      │
              │ stream{}        │
              └────────┬────────┘
                       │ → 127.0.0.1:8443
                       ▼
              ┌─────────────────┐
              │ authproxy       │
              │ - 校验 session  │
              │ - 路由到 relay  │
              └────────┬────────┘
                       │ → 127.0.0.1:<svc_relay_port>
                       ▼
              ┌─────────────────┐
              │ rathole-server  │
              └────────┬────────┘
                       │ 反向隧道
                       ▼
              ┌─────────────────┐
              │ 跳板机          │
              │ rathole-client  │
              └────────┬────────┘
                       │
                       ▼
                  service.target_addr:port
```

每个本地 accept 独立开一个 CONNECT 隧道,完了双向 io.Copy 直到任一端关闭。

## 前置条件

1. 本地 `quicktun login` 时配过 `--auth-proxy <host:port>`(否则会报 `credentials missing auth_proxy_endpoint`)
2. service 已分配 relay port(默认 create 时立即分配)
3. session token 有效(没过期 / 没被 logout)

## 错误

| 错误 | 原因 |
|---|---|
| `credentials missing auth_proxy_endpoint` | 没配 auth-proxy,重跑 `quicktun login --auth-proxy <host>` |
| `service ... has no allocated relay port` | service 状态异常,检查控制面日志 |
| `forward: auth-proxy rejected (401)` | session token 过期或 service 不在你的可访问范围,重新 login |
| `accept: ...` | 本地端口被占;换一个 `--local-port` |
| `forward: dial auth-proxy ...: connection refused` | auth-proxy / nginx 没起,或 endpoint 错 |

## 看源码

`cmd/quicktun/cmd_forward.go` — handleForward 镜像了 `internal/agent/bridge.go` 的 CONNECT 客户端逻辑,只是 token 换成 operator session、target 换成 service relay 端口。
