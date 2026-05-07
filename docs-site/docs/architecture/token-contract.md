---
sidebar_position: 4
---

# Token 合约

quicktun 的 site agent token 有一个**关键不变量**:**raw 永远不入库**。这一页讲为什么、怎么实现的。

## 核心问题

site agent token 要被两个完全不同的消费者使用:

1. **agent → control plane**:gRPC API 调 `Authorization: Bearer <token>`
2. **rathole-client → rathole-server**:rathole 协议本身要求两端有相同的 shared secret

如果 control plane 和 rathole 用不同的 token,operator 就要管两个;如果用同一个 raw token,DB 里就要存明文。两个都不行。

## 设计

**一个 token,两种呈现形式**:

```
Operator 看到:        raw_token  (32 bytes hex / base64)
Control plane 存:     sha256_hex(raw_token)
agent 跑时持有:        raw_token  (从配置文件读)
agent → control plane: Authorization: Bearer <raw>
agent → rathole-server: token = sha256_hex(raw)   (rathole-client.toml)
control plane → rathole-server: token = sha256_hex(raw)   (rathole-server.toml)
```

## 颁发流程

```
[Operator]                [Control Plane]                  [DB]
   │                            │                            │
   │ quicktun site               │                            │
   │ get-install-command         │                            │
   │ ───────────────────────────►│                            │
   │                             │ generate 32 random bytes  │
   │                             │ raw = hex.Encode(bytes)   │
   │                             │ hash = sha256_hex(raw)    │
   │                             │ ───────────────────────►  │
   │                             │   INSERT INTO            │
   │                             │   site_agent_tokens       │
   │                             │   (token_hash = hash)     │
   │                             │                            │
   │ ◄── raw token (一次) ───────│                            │
   │                             │                            │
   │ raw 进 install-agent.sh                                   │
   │ raw 写入 /etc/quicktun/agent.yaml                         │
```

DB schema:

```sql
CREATE TABLE site_agent_tokens (
    id          INTEGER PRIMARY KEY,
    site_id     INTEGER NOT NULL,
    token_hash  TEXT NOT NULL,        -- sha256_hex(raw)
    expire_time DATETIME,
    -- raw 永远不在这里
);
```

## 校验流程

### Agent → Control Plane(gRPC)

`internal/auth/agent_interceptor.go`:

```go
// 收到 Authorization: Bearer <raw>
hash := sha256.Sum256([]byte(raw))
hex := hex.EncodeToString(hash[:])
db.Where("token_hash = ?", hex).First(&tok)
// → site_id → site → project_id
```

### Agent → auth-proxy(HTTP CONNECT)

走相同逻辑(`internal/authproxy/router.go::Route` 调 `dao.SiteAgentTokenDAO.ValidateRaw`)。

### Agent → rathole-server(rathole 协议)

agent 的 `rathole-client.toml` 里 `token = "<sha256_hex(raw)>"`;control plane 渲染的 `rathole-server.toml` 里也是 `token = "<sha256_hex(raw)>"`(`internal/relay/render.go`)。

rathole 自己的握手协议比对 shared secret,不需要再查 DB。两端都没碰过 raw,但能通信。

## 为什么不直接给 operator 看 hash?

考虑过,但有几个问题:

- operator 复制粘贴 hash 时容易出错(64 hex,看不出错没错)
- 硬性要求"agent 配置里写的就是 control plane API 的 Bearer token",hash 还要在 agent 启动时反查 raw,哪都没有这个映射
- raw → hash 是单向的:operator 一旦丢了 raw,只能 rotate 一个新的(`quicktun site rotate-agent-token`)

所以**只把 raw 一次性吐给 operator,DB 永远只有 hash**,这个不变量牢靠且 operator UX 友好。

## Rotate

```bash
quicktun site rotate-agent-token clinic-net/bastion-1
```

新增 token row(新 hash + 新 raw 给 operator),旧 row 标记 `revoked=true`(或直接删 — 看 DAO 实现)。控制面 supervisor 监听 token 变化 → 重 render rathole-server.toml → restart rathole-server。

agent 端要重启拿新 token 才能继续连(rathole 握手会失败 → supervisor 退出 → 重启 → bootstrap 失败 → exponential backoff)。Phase 1 不做 token 平滑切换;rotate 是个 schedule maintenance 操作。

## 实现要点

- 32 bytes 真随机(crypto/rand),编码为 64 hex
- sha256 用 hex 编码,与 rathole 配置文件易于人眼比对
- token 没有 prefix(不像 GitHub `ghp_xxx`),完全 opaque
- 每个 site 一个 active token;rotate 后旧 token 即时失效

更深入的源码导读:

- DAO:`internal/dao/site_agent_token.go`
- 颁发 API:`internal/grpcsvc/site_service.go::GetSiteInstallCommand` / `RotateSiteAgentToken`
- agent 校验:`internal/auth/agent_interceptor.go`
- auth-proxy 校验:`internal/authproxy/router.go`
- 渲染:`internal/relay/render.go`(server)+ `internal/agent/render.go`(client)
