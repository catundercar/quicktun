---
sidebar_position: 1
---

# 5 分钟概念

如果你完全没用过 quicktun,这一页让你**在动手之前**先理解整体形态。

## 你需要哪些机器

| 角色 | 机器 | 用途 |
|---|---|---|
| **控制面** | 1 台公网 Linux VPS | 跑 `quicktun-server` + `quicktun-authproxy` + `rathole`(server 模式)+ nginx 终结 TLS |
| **跳板机(每个 site)** | N 台 Linux / macOS / Windows | 跑 `quicktun-agent` + `rathole-client`,一只脚连公网控制面,一只脚连项目内网 |
| **Operator 工作站** | 你的笔记本 | 跑 `quicktun` CLI |

## 你需要哪些二进制

quicktun 编译出 4 个二进制:

| 二进制 | 跑在哪 | 干啥 |
|---|---|---|
| `quicktun-server` | 控制面 | gRPC + HTTP 网关 + DB + supervisor |
| `quicktun-authproxy` | 控制面 | HTTP CONNECT 网关,验 token 后把 TCP 转到本机 rathole-server |
| `quicktun-agent` | 跳板机 | bootstrap / heartbeat / 渲染 rathole-client.toml / supervise rathole-client |
| `quicktun` | 工作站 | operator CLI |

外加一个**外部依赖**:`rathole`(从 [rapiz1/rathole](https://github.com/rapiz1/rathole/releases) 下载二进制)。

## 流程概览

```
[Admin]                    [Control Plane]                   [Bastion]                 [Operator]
   │                              │                              │                        │
   │ 1. quicktun login            │                              │                        │
   │─────────────────────────────►│                              │                        │
   │ 2. quicktun project create   │                              │                        │
   │─────────────────────────────►│ → rathole-server 启动        │                        │
   │ 3. quicktun site create      │                              │                        │
   │─────────────────────────────►│                              │                        │
   │ 4. quicktun service create   │                              │                        │
   │─────────────────────────────►│ → 分配 relay_port            │                        │
   │ 5. site get-install-command  │                              │                        │
   │─────────────────────────────►│                              │                        │
   │     ◄─── raw token ──────────│                              │                        │
   │                              │                              │                        │
   │  6. install-agent.sh --token │                              │                        │
   │─────────────────────────────────────────────────────────────►│                        │
   │                              │                              │ Bootstrap (Bearer raw) │
   │                              │◄─────────────────────────────│                        │
   │                              │ 渲染 rathole-client.toml    │                        │
   │                              │ 启动 rathole-client         │                        │
   │                              │◄════════ 反向隧道 ══════════│                        │
   │                              │                              │                        │
   │                              │                              │   7. quicktun forward  │
   │                              │◄─────────────────────────────────────────────────────│
   │                              │ auth-proxy: CONNECT + Bearer │                        │
   │                              │ → rathole → site → LAN      │                        │
```

## 上手步骤(高层)

1. **控制面装机**:Linux VPS + DNS 解析 + nginx + cert + `make build` + `./deploy/install-server.sh`。详见 [安装控制面](./install-server.md)。
2. **创建 project / site / service**:operator 用 `quicktun login` 登录控制面后,创建资源并拿到 install token。
3. **跳板机装 agent**:在每个网点的跳板机上跑 `./deploy/install-agent.sh --token <RAW> --control-endpoint <host>`,详见 [安装 agent](./install-agent.md)。
4. **Operator 用 forward**:`quicktun forward project/site/service --local-port 2222`,然后 `ssh -p 2222 user@127.0.0.1`。

## 资源命名

quicktun 用 Google AIP 风格的资源命名:

```
projects/<slug>
projects/<slug>/sites/<slug>
projects/<slug>/sites/<slug>/services/<slug>
```

CLI 接受**完整 6 段**或**简写 3 段**两种形态:

```bash
# 完整
quicktun service get projects/clinic-net/sites/bastion-1/services/ssh

# 简写
quicktun service get clinic-net/bastion-1/ssh
```

后续文档统一用简写形式。
