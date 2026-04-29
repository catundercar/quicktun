# quicktun 总体设计

> 内部运维工具。替代 TeamViewer/ToDesk 的多网点远程访问，让多项目内网通过统一控制面被 SSH/RDP/AI 工具直连。

## 1. 产品定位

- **内部运维工具**，不是 SaaS。使用者 = 团队运维成员
- 解决问题：替代 TeamViewer/ToDesk，让多项目网点能被 SSH 远程 + AI 工具（Cursor / Claude Code）直连
- Phase 1：rathole 后端 + 单点（endpoint）模式 + auth-proxy 网络层准入
- Phase 2+：NetBird 后端、子网模式、agent 自动化

## 2. 整体架构

```
┌──────────────────────────────────────────────────────┐
│              quicktun-server (公网 VPS)              │
│                                                       │
│  ┌────────────────────────────────────────────────┐  │
│  │  Control Plane (gRPC + grpc-gateway + CLI)    │  │
│  │  - 鉴权 / 项目 / site / service 管理          │  │
│  │  - operator session / token 颁发              │  │
│  │  - 审计日志                                    │  │
│  │  - SQLite 存储（GORM）                         │  │
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
│                                                       │
└──────────────────────┬───────────────────────────────┘
                       │ TCP/443 (TLS + token)
                       │ 反向隧道
   ┌───────────────────┼───────────────────────┐
   │                   │                       │
┌──▼──────────┐  ┌────▼─────────┐  ┌──────────▼─┐
│ Site A 跳板机│  │ Site B 跳板机│  │ Site C 跳板机│
│ + agent     │  │ + agent      │  │ + agent      │
│ + rathole-  │  │ + rathole-   │  │ + rathole-   │
│   client    │  │   client     │  │   client     │
└──────┬──────┘  └──────────────┘  └──────────────┘
       │ 转发到内网
       ▼
┌──────────────────────┐
│ 项目内网 LAN          │
│ 192.168.x.0/24       │
└──────────────────────┘

┌──────────────┐
│ Operator     │ ssh hospital-01
│ 笔记本       │   ↓ ProxyCommand
│ Cursor / CC  │ quicktun connect → TLS:443 (token) → auth-proxy → rathole → site → LAN
└──────────────┘
```

## 3. 核心概念

| 概念 | 含义 | 备注 |
|---|---|---|
| Operator | 运维成员 | Phase 1 个位数账号 |
| Project | 一个客户/业务的网点集合 | 数据隔离边界 + relay 进程隔离边界 |
| Site | 一台跳板机（一只脚公网，一只脚项目内网） | mode 字段预留，Phase 1 只用 endpoint |
| Service | site 暴露的端口；target 可以是跳板机本身或内网任意 IP | 例 `192.168.10.50:22` |
| Backend | rathole / (netbird) | 接口抽象，Phase 1 只实现 rathole |
| Session | operator 一次登录会话 | bearer token + 8h 过期 |

## 4. Phase 1 范围

### 实现
- 控制面（gRPC + grpc-gateway，GORM + SQLite）
- quicktun-auth-proxy（自研，~1k 行 Go）
- rathole 集成（绑 127.0.0.1，控制面 supervisor）
- quicktun-agent（Linux + Windows）
- quicktun CLI（operator 用）

### 非目标
- ❌ NetBird backend（接口预留）
- ❌ 子网模式（schema 留字段，CLI 不暴露）
- ❌ 程序化 API token（CI/自动化场景出现时再加）
- ❌ Web UI（CLI 够用）
- ❌ 区分人 vs AI 的细粒度审计
- ❌ 多租户 SaaS / 客户自助登录
- ❌ 内网自动扫描发现（service 由 operator 手动 add）
- ❌ agent 自更新（手动触发即可）
- ❌ 应用层认证（SSH key/cert/RDP 凭据由各 site 自己管理，quicktun 不碰）

## 5. 安全分层

| 层 | 谁负责 | 怎么做 |
|---|---|---|
| **网络层准入**（"谁能 TCP 连到 relay"） | quicktun | auth-proxy 前置 + bearer token over TLS（详见 04-security.md） |
| **应用层认证**（"连上后用什么身份"） | site 自己 | 各 site 的 sshd / RDP / DB 凭据由现有运维实践管理；quicktun 不动 |

**核心边界**：quicktun 是 TCP 反向代理 + 网络准入，**不参与应用层认证**。这保证了协议无关性（SSH/RDP/DB/HTTP 同等对待）和低侵入性（跳板机 sshd 不动、LAN 机器不动）。

## 6. 关键决策记录

| # | 决策 | 理由 |
|---|---|---|
| 1 | Phase 1 只做 rathole | 国内网络 TCP 反向代理最稳；endpoint 模式覆盖 80% 场景 |
| 2 | Backend 抽象 day 1 就做 | 数据模型用通用概念，Phase 2 接 NetBird 只新增 driver |
| 3 | Project 是数据 + relay 隔离边界 | 防止跨客户配置串扰；将来单客户迁出容易 |
| 4 | 内部工具，无客户角色 | 简化 auth，audit 只记 control plane 操作 |
| 5 | 砍掉 API token | AI 工具走 SSH config，不需要调控制面 API |
| 6 | Site = 跳板机，service.target 显式声明 | 反映真实拓扑；为 Phase 2 subnet 模式打底 |
| 7 | 每个 project 独立 relay 进程 | 故障隔离，端口段独立 |
| 8 | 配置完全由控制面下发，agent 不可本地修改 | 防止跳板机被入侵后攻击者横向 |
| 9 | 语言选 Go | 单二进制部署；交叉编译友好；containerd/sshd 生态库成熟 |
| 10 | quicktun 不实现 SSH key/cert/authorized_keys 同步 | 反向代理不该做应用层认证；LAN 机器不动 |
| 11 | rathole supervisor 用 Go `os/exec` 子进程 | Phase 1 项目数小，零新依赖；控制面本身由 systemd 拉起 |
| 12 | Phase 1 不做 agent 自更新 | 多一套机制 × 多一套 bug 面；手动触发够用 |
| 13 | quicktun 边界 = 网络层可达性 + 项目隔离 + 网络准入 | 应用层认证（SSH/RDP/DB）由各 site 自己管理 |
| 14 | 网络层准入用 auth-proxy + 短期 token，**放弃 IP 白名单** | 办公网/CGNAT 普遍 NAT，IP 不是有效凭据 |
| 15 | 自研 quicktun-auth-proxy（~1k 行 Go），rathole 保留但绑 127.0.0.1 | rathole 不暴露公网；token 校验集中在 auth-proxy |
| 16 | API 用 gRPC + grpc-gateway，遵循 Google API Design Guide | proto 单一来源；HTTP/JSON 透传给 CLI；强 schema |
| 17 | ORM 用 GORM | Active-record 模式，迭代快；Phase 1 不需要 sqlc 的极致类型安全 |
| 18 | 日志库用 zap (Uber) + lumberjack（rotate） | 结构化、零分配热路径；子进程 stdout 包装成 zap 事件 |
| 19 | 控制面 auth 用 email + password | 内部工具，bcrypt 即可；2FA 留 Phase 2 |
| 20 | 操作系统支持 Linux + Windows agent | Windows 侧用 Microsoft OpenSSH；agent 内有 Platform 抽象 |

## 7. 文档导航

| 文档 | 内容 |
|---|---|
| [00-overview.md](./00-overview.md) | 本文档：产品定位、整体架构、决策记录 |
| [01-data-model.md](./01-data-model.md) | GORM 数据模型 + ER 图 + SQLite schema |
| [02-grpc-api.md](./02-grpc-api.md) | gRPC + grpc-gateway 接口设计（遵循 Google API Design Guide）|
| [03-agent-protocol.md](./03-agent-protocol.md) | 控制面 ↔ agent 协议（注册、心跳、配置拉取）|
| [04-security.md](./04-security.md) | 网络层准入、auth-proxy、token 颁发、跳板机加固 |
| [05-process-supervisor.md](./05-process-supervisor.md) | rathole / auth-proxy 子进程管理 |
| [06-cli.md](./06-cli.md) | operator CLI 命令设计 |
| [07-deployment.md](./07-deployment.md) | quicktun-server / agent 部署 |
| [08-roadmap.md](./08-roadmap.md) | Phase 1 里程碑 + Phase 2/3 路标 |
