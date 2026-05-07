---
slug: /intro
sidebar_position: 1
---

# 简介

**quicktun** 是一个内部运维工具,用于替代 TeamViewer / ToDesk 等远程访问工具,让团队能够通过统一的控制面 SSH 远程登录、连接 AI 工具(Cursor、Claude Code)到多个项目网点的内网。

## 解决什么问题

如果你需要管理多个客户/项目的网点(医院网络、工厂车间、客户机房等),传统做法常常踩这些坑:

- **每个网点独立 VPN / 内网穿透**:配置散落,operator 难以统一管理
- **TeamViewer / ToDesk** 屏幕共享:只能图形化操作,SSH / scp / AI 工具用不了
- **IP 白名单**:CGNAT、移动网络下 IP 漂移,白名单失效
- **跨网点权限**:不同 operator 是否能访问不同 project,缺乏统一管控

quicktun 的设计目标:

1. 一台跳板机一只脚公网、一只脚内网,通过反向隧道连接到中央控制面
2. 控制面统一颁发 token、管理 project / site / service
3. operator 一行 `quicktun forward` 就能把内网端口映射到本地,SSH / RDP / Cursor / 数据库客户端都能直连
4. 应用层认证(SSH key / RDP 密码)由各 site 自己管理,quicktun 只做**网络层准入**

## 适用场景

| 场景 | 是否合适 |
|---|---|
| 5 ~ 50 个客户网点的统一运维 | ✅ |
| 团队内 AI 工具(Cursor / Claude Code)远程接入客户内网 | ✅ |
| 替代单点 frp / ngrok / 多机异构 | ✅ |
| 大规模 SaaS 场景(>1000 网点) | ❌(Phase 1 不做 HA) |
| 终端用户屏幕共享 | ❌(应该用 TeamViewer) |
| Web UI 自助开通 | ❌(operator 是 CLI,无 UI) |

## 整体架构(一图)

```
┌──────────────────────────────────────────────────────┐
│            quicktun-server (公网 VPS)                 │
│  ┌────────────────────────────────────────────────┐  │
│  │  Control Plane (gRPC + grpc-gateway)           │  │
│  │  - 鉴权 / 项目 / site / service 管理           │  │
│  │  - operator session / token 颁发               │  │
│  │  - SQLite 存储(GORM)                          │  │
│  └────┬──────────────────┬────────────────────────┘  │
│       │ supervises       │ supervises                │
│       ▼                  ▼                            │
│  ┌─────────┐    ┌──────────────────┐                 │
│  │ rathole │    │ quicktun-auth-   │                 │
│  │ (绑 127 │    │ proxy            │                 │
│  │ .0.0.1) │    │ :443 (TLS)       │                 │
│  └─────────┘    │ token 校验       │                 │
│       ▲         │ → splice 到 127  │                 │
│       └─────────┴──────────────────┘                 │
└──────────────────────┬───────────────────────────────┘
                       │ TCP/443 (TLS + token)
                       │ 反向隧道
   ┌───────────────────┼───────────────────────┐
   │                   │                       │
┌──▼──────────┐  ┌────▼─────────┐  ┌──────────▼─┐
│ Site A 跳板机│  │ Site B 跳板机│  │ Site C 跳板机│
│ + agent      │  │ + agent      │  │ + agent      │
└──────┬──────┘  └──────────────┘  └──────────────┘
       │ 转发到内网
       ▼
  项目内网 LAN
```

## 核心概念

| 概念 | 含义 |
|---|---|
| **Operator** | 运维成员,通过 CLI 登录后操作 |
| **Project** | 一个客户/业务的网点集合,数据隔离 + relay 进程隔离 |
| **Site** | 一台跳板机(一只脚公网,一只脚项目内网) |
| **Service** | site 上暴露的端口 + 协议(TCP / UDP),target 可以是跳板机本身或内网任意 IP |
| **Backend** | 反向隧道驱动,Phase 1 只实现 rathole |
| **Session** | operator 一次登录会话,bearer token + 过期时间 |

## Phase 1 状态

quicktun 当前处于 **Phase 1 (MVP)**,目标是端到端跑通 5 ~ 10 个 site:

- ✅ 控制面 gRPC + grpc-gateway
- ✅ rathole 集成 + auth-proxy 网络层准入
- ✅ Linux / macOS / Windows agent
- ✅ Operator CLI(`quicktun forward` / `project` / `site` / `service` / `status`)
- ✅ systemd / launchd / NSSM 部署脚本
- ❌ NetBird backend、Web UI、HA、2FA(留 Phase 2+)

继续阅读 [快速开始](./getting-started/overview.md) 了解 5 分钟上手流程。
