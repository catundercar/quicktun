---
sidebar_position: 1
---

# 架构总览

## 系统组件

quicktun 是一个分布式系统,有 4 个进程类型:

```
+---------------------------------------------------+
|       quicktun-server (公网 VPS)                  |
|                                                   |
|  ┌─────────────────────────────────────────────┐  |
|  │  Control Plane                              │  |
|  │  - gRPC :9090 (operator + agent)           │  |
|  │  - grpc-gateway :9091 (REST mirror)        │  |
|  │  - Auth / Project / Site / Service / Admin │  |
|  │  - SQLite + GORM                           │  |
|  │  - relay.Manager (supervisor for rathole)  │  |
|  │  - sweeper (stale → offline)               │  |
|  └────┬───────────────────────────┬────────────┘  |
|       │ supervises                │ supervises    |
|       ▼                           ▼               |
|  ┌─────────────┐          ┌──────────────────┐    |
|  │ rathole     │          │ quicktun-        │    |
|  │ (server)    │          │ authproxy        │    |
|  │ 127.0.0.1   │          │ 127.0.0.1:8443   │    |
|  │ :20000+     │          │ HTTP CONNECT     │    |
|  └─────▲───────┘          └────────┬─────────┘    |
|        │ loopback                  │ loopback     |
|        └──────────────────┬────────┘              |
|                           │                       |
|              ┌────────────▼────────────┐          |
|              │  nginx (TLS terminator) │          |
|              │  :443 stream + http     │          |
|              └────────────┬────────────┘          |
+----------------------------|----------------------+
                             │ TCP/443 TLS
            ┌────────────────┼────────────────┐
            │                │                │
       ┌────▼────┐      ┌────▼────┐      ┌────▼────┐
       │ Site A  │      │ Site B  │      │ Site C  │
       │ agent + │      │ agent + │      │ agent + │
       │ rathole-│      │ rathole-│      │ rathole-│
       │ client  │      │ client  │      │ client  │
       └────┬────┘      └─────────┘      └─────────┘
            │ 转发到内网 LAN
            ▼
       192.168.x.0/24
```

## 组件职责

| 组件 | 部署在 | 职责 |
|---|---|---|
| `quicktun-server` | 公网 VPS | 控制面 gRPC + grpc-gateway;Auth / Project / Site / Service / Agent / Admin 服务;SQLite 状态存储;rathole-server 子进程监管;site sweeper |
| `rathole`(server) | 公网 VPS,绑 127.0.0.1 | 反向隧道服务端,每个 project 一个进程,绑定本机的 `relay_port_range` |
| `quicktun-authproxy` | 公网 VPS,绑 127.0.0.1 | HTTP CONNECT 网关。校验 Bearer token(site agent token / operator session),把 TCP 转发到对应 project 的 rathole loopback 地址 |
| `nginx` | 公网 VPS | 终结 TLS;`http {}` 反代 gRPC 到 9090,grpc-gateway 到 9091;`stream {}` 反代 :443 到 authproxy:8443 |
| `quicktun-agent` | 跳板机 | bootstrap 拉控制面配置 → 渲染 `rathole-client.toml` → supervise rathole-client 子进程;15s heartbeat 保持 `last_seen_time` |
| `rathole`(client) | 跳板机 | 反向隧道客户端,通过 in-process bridge → authproxy(CONNECT + Bearer)→ rathole-server |
| `quicktun` CLI | operator 工作站 | login / project / site / service / forward / status |

## 数据流

### Bootstrap(agent 启动)

```
agent              control-plane                   DB
  │                       │                         │
  │ Bootstrap(Bearer raw) │                         │
  │──────────────────────►│ sha256(raw) lookup      │
  │                       │────────────────────────►│
  │                       │◄────── token row ───────│
  │ ◄── BootstrapResp ────│ (auth_proxy_endpoint,   │
  │                       │  tunnels, config_ver)   │
  │                       │                         │
  │ render rathole-client.toml                      │
  │ start rathole-client                            │
```

### Operator 转发

```
operator                 authproxy        rathole-server     agent           service target
   │ quicktun forward       │                  │                │                  │
   │ accept localhost:2222  │                  │                │                  │
   │ ssh -p 2222            │                  │                │                  │
   │                        │                  │                │                  │
   │  CONNECT 127.0.0.1:N   │                  │                │                  │
   │  Bearer <session>      │                  │                │                  │
   │───────────────────────►│ validate token   │                │                  │
   │                        │ → service relay  │                │                  │
   │                        │ port lookup      │                │                  │
   │                        │─────────────────►│ relay_port → service tunnel       │
   │                        │                  │───────────────►│                  │
   │                        │                  │                │ ──── TCP ───────►│
   │ ◄══════════════════ bidirectional io.Copy ══════════════════════════════════►│
```

### 反向隧道(agent → rathole-server)

```
agent                    bridge (127.0.0.1:R)    authproxy (loopback)   rathole-server
  │                              │                         │                    │
  │ rathole-client connect       │                         │                    │
  │ → 127.0.0.1:R                │                         │                    │
  │─────────────────────────────►│                         │                    │
  │                              │ CONNECT relay:443       │                    │
  │                              │ Bearer raw              │                    │
  │                              │────────────────────────►│ token validate    │
  │                              │                         │ → project port    │
  │                              │                         │──────────────────►│
  │                              │                         │ HTTP/1.1 200 OK   │
  │                              │◄────────────────────────│                    │
  │ ◄════════════ rathole protocol bytes 双向 io.Copy ════════════════════════►│
```

## 安全分层

| 层 | 谁负责 | 怎么做 |
|---|---|---|
| **网络层准入**("谁能 TCP 到 relay") | quicktun | auth-proxy 前置 + Bearer over TLS;site agent token 与 operator session token 两类凭据 |
| **应用层认证**("连上后用什么身份") | site 自己 | 各 site 的 sshd / RDP / DB 凭据由现有运维实践管理;quicktun 不动 |

quicktun 是 TCP 反向代理 + 网络准入,**不参与应用层认证**。这保证了协议无关性(SSH/RDP/DB/HTTP 同等对待)和低侵入性(跳板机 sshd 不动、LAN 机器不动)。

## 关键决策(摘要)

完整决策记录见仓库内 `docs/00-overview.md`。摘 7 条:

1. Phase 1 只做 **rathole**,因为国内网络 TCP 反向代理最稳
2. **每个 project 独立 rathole 进程**,故障隔离 + 端口段独立
3. 配置完全由控制面下发,**agent 不可本地修改**(防跳板机被入侵后横向)
4. **网络层准入用 token,放弃 IP 白名单**(CGNAT 普遍 NAT)
5. **rathole 绑 127.0.0.1**,公网入口由自研 auth-proxy 处理(token 校验集中)
6. API 用 **gRPC + grpc-gateway**(Google AIP 风格)
7. ORM 用 **GORM**,日志用 **zap + lumberjack**

## 继续阅读

- [auth-proxy 设计](./auth-proxy.md) — HTTP CONNECT 协议、双 token 路由
- [agent 内部](./agent.md) — bootstrap / heartbeat / render / supervisor / bridge
- [Token 合约](./token-contract.md) — raw vs sha256(raw) 的存储与展示
