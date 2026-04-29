# 安全设计

## 1. 安全模型

quicktun 是一个 **TCP 反向代理 + 网络层准入控制器**，不参与应用层认证。

### 1.1 责任分层

| 层 | 谁负责 | 实现 |
|---|---|---|
| **网络层准入**（"谁能 TCP 连到 relay"） | quicktun | quicktun-auth-proxy + bearer token over TLS |
| **应用层认证**（"连上后用什么身份"） | site / 用户自己 | 各 site 的 sshd / RDP / DB 凭据自管 |

### 1.2 信任域

```
公网（不可信）
  ↕ TLS
quicktun-server（可信，团队运维拥有）
  ↕ TCP/443 反向隧道（TLS）
跳板机 quicktun-agent（半可信：被入侵的话攻击者拿到 LAN 入口）
  ↕ TCP（受 agent 转发规则约束）
LAN 目标机器（信任域取决于客户场景）
```

## 2. 网络层准入

### 2.1 为什么不能用 IP 白名单

办公网 / 家宽 / 4G 普遍是 NAT，**出口 IP 不是有效凭据**：

- 办公网共享 IP → 整个办公室所有设备都获得授权
- CGNAT → 同小区 / 楼栋几百到几千户共享 IP
- 4G / 咖啡店 WiFi → 同基站 / 现场所有人共享
- SDWAN → 出口 IP 在数据中心间漂移，连自己都连不稳

→ **完全放弃 IP 白名单方案**。

### 2.2 quicktun-auth-proxy

新组件，约 800-1200 行 Go，做的事：

```
┌──────────────────────────────────────────────┐
│ Operator 笔记本                              │
│  ssh hospital-01                             │
│    ↓ ProxyCommand quicktun connect %h        │
│  quicktun connect:                           │
│    1. 读 ~/.quicktun/token                   │
│    2. tls.Dial relay.example.com:443         │
│    3. 发首帧 {token, target: hospital-01}    │
│    4. 双向 splice stdin/stdout ↔ TCP socket  │
└──────────────────┬───────────────────────────┘
                   │ TLS:443
                   ▼
┌──────────────────────────────────────────────┐
│ quicktun-server                              │
│ ┌──────────────────────────────────────────┐ │
│ │ quicktun-auth-proxy :443                 │ │
│ │  1. TLS accept                           │ │
│ │  2. 读首帧 {token, target}               │ │
│ │  3. 校验 token → 解析 operator           │ │
│ │  4. 检查 operator 对 target 的访问权     │ │
│ │  5. 查找 target site 的 rathole 端口     │ │
│ │  6. dial 127.0.0.1:<rathole_port>        │ │
│ │  7. 双向 splice                          │ │
│ │  8. audit_log（连接打开/关闭）           │ │
│ └────────────────────┬─────────────────────┘ │
│                      ▼                       │
│ ┌──────────────────────────────────────────┐ │
│ │ rathole-server (绑 127.0.0.1)            │ │
│ │  - project A: 127.0.0.1:20000-20999      │ │
│ │  - project B: 127.0.0.1:21000-21999      │ │
│ └──────────────────────────────────────────┘ │
└──────────────────────────────────────────────┘
```

### 2.3 协议帧

auth-proxy 入站协议（TLS 之上）：

```
+------------------+------------------+----------------------+
| 4 bytes  length  | 1 byte version=1 | json {token, target} |
+------------------+------------------+--------------+-------+
                                                     ↓
                                       之后是裸 TCP（透传给后端 rathole）
```

`target` 用资源 name：`projects/clinic-network/sites/hospital-01/services/ssh`

### 2.4 token 颁发

operator 通过 `AuthService.Login`（见 [02-grpc-api.md](./02-grpc-api.md)）拿到 access_token，写入 `~/.quicktun/token`，默认 8h 过期。

**token 形态**：
- 32 字节随机 → URL-safe base64
- DB 存 SHA-256 hash
- 客户端只在颁发瞬间见到原文

```go
func IssueToken() (raw, hash string) {
    b := make([]byte, 32)
    rand.Read(b)
    raw = base64.URLEncoding.EncodeToString(b)
    h := sha256.Sum256([]byte(raw))
    hash = hex.EncodeToString(h[:])
    return
}
```

### 2.5 token 校验缓存

auth-proxy 是热路径，每次连接都查 DB 太重。

**方案**：内存缓存 + 控制面通过本地 RPC 主动 invalidate。

```go
type TokenCache struct {
    mu    sync.RWMutex
    cache map[string]*CachedSession // key = hash
    ttl   time.Duration             // 60s 强制过期重查
}

// 控制面 logout / session revoke 时调本地 RPC：
//   POST /internal/cache/invalidate { token_hash }
// auth-proxy 收到立刻清 cache
```

正常路径：cache 命中 → 微秒级。Cache miss → 一次 SQLite 查询。

### 2.6 撤销

- **operator 主动 logout**：控制面置 `revoked_at = now`，invalidate cache
- **管理员撤销**：admin API 撤销任意 session
- **过期**：每分钟 cron 扫 `expires_at < now AND revoked_at IS NULL`，更新 + invalidate cache

## 3. agent ↔ 控制面安全

- 入站协议详见 [03-agent-protocol.md](./03-agent-protocol.md)
- 站点 agent 用 `site_agent_token`（长期）认证
- agent 注册时拿到 token 后，join_token 立即作废
- TLS 强制，证书可用 Let's Encrypt 自动签发

## 4. 跳板机加固

跳板机是攻击高价值目标——拿下它能横向打整个 LAN。最小安全实践：

1. **agent 进程最小权限**：专用用户 `qt-agent`，无 sudo，systemd 单元 `ProtectSystem=strict` `NoNewPrivileges=true`
2. **agent 只代理控制面已声明的 service**：`agent_config.services` 之外的端口不开转发
3. **agent 禁止本地修改配置**：所有配置由控制面下发，agent 验证签名（Phase 2 加签名）
4. **rathole-client 出站连接**：只对控制面 relay.example.com:443，DNS 失败不 fallback
5. **service 添加要 audit_log 留痕**：谁在什么时候给哪个 site 加了对哪个 LAN IP 的访问，必须可查（详见 [02-grpc-api.md](./02-grpc-api.md) Audit）

## 5. 控制面加固

| 项 | 措施 |
|---|---|
| password 存储 | bcrypt cost=12 |
| token 存储 | SHA-256 hash，原文不入库 |
| TLS | Let's Encrypt + cert-manager / certbot 自动续签；最低 TLS 1.2 |
| API 速率 | login `/auth:login` 限 5/min/IP；其他 API 100/min/operator |
| 审计完整性 | audit_log 只追加，不允许 update/delete；管理员操作也记 |
| SQLite 文件 | 0600，owner=quicktun |
| systemd 单元 | `ProtectSystem=strict`、`NoNewPrivileges`、`MemoryMax`、`TasksMax` 限额 |
| 备份 | sqlite-backup-cron 每日打包 + 加密上传 |

## 6. 威胁建模快查

| 威胁 | 防护 | 残余风险 |
|---|---|---|
| 互联网随机端口扫描 | rathole 绑 127.0.0.1，公网只看到 443 / 9443 / 9080 | 无 |
| 攻击者拿到 relay 地址尝试爆破 | auth-proxy 必须 token，错误次数限制 | 弱 token 暴力（用强 token + bcrypt password） |
| 笔记本被盗 | token 8h 过期；可主动 logout 撤销 | 8h 窗口；需要团队规范"丢失立刻 logout" |
| 跳板机被入侵 → LAN 横向 | agent 配置由控制面下发，攻击者无法新增转发端口 | 攻击者可用现有 service 进 LAN，应由 LAN 侧应用层认证拦截 |
| 控制面被入侵 | 所有 token / agent_token 失效；rathole 配置可能被改 | 灾难级；需要监控 + 备份 + 入侵检测（Phase 2） |
| 共享 NAT 同 IP 用户冒充 | token 是密码学凭据，与 IP 无关 | 无 |
| 中间人攻击 | TLS 强制 + 证书校验 | 自签名证书需 pin（CLI 首次连接 prompt 信任） |
| Replay 攻击 | TLS 防止；token 自身仅 bearer 形态可能被截 → 但 TLS 已防 | 无 |

## 7. Phase 2 安全增强（路标）

- 控制面 2FA（TOTP）
- API token 颗粒度到 scope（site:read / site:write）
- 审计日志数字签名
- 密钥存到 HSM / KMS（CA 私钥 / TLS 私钥）
- agent 配置签名验证
- IDS / Suricata 集成
- HTTPS 健康检查（uptime monitoring）
