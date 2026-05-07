---
sidebar_position: 2
---

# auth-proxy 设计

`quicktun-authproxy` 是一个**纯 Go 实现的 HTTP CONNECT 网关**(~1k 行),作为公网到 rathole 的唯一入口。

## 协议

每个客户端(agent 或 operator CLI)通过单个 TCP 连接发送一个 CONNECT 请求:

```
CONNECT <target> HTTP/1.1\r\n
Authorization: Bearer <token>\r\n
Host: <relay-host>:443\r\n
\r\n
```

auth-proxy 收到后:

1. 解析 HTTP 请求行 + 头
2. 提取 `Authorization: Bearer <token>`,**根据 token 类型**路由(详见下文)
3. 校验通过 → 拨号到 backend(`127.0.0.1:<port>`)
4. 返回 `HTTP/1.1 200 OK\r\n\r\n`
5. **双向 io.Copy**,直到任一方关闭

校验失败:

- 缺 Authorization、token 错、过期、project 已禁用 → `HTTP/1.1 401 Unauthorized`
- 不是 CONNECT 方法 → `HTTP/1.1 405 Method Not Allowed`
- backend 拨号失败 → `HTTP/1.1 502 Bad Gateway`

## 双 token 类型

auth-proxy 同时为两类客户端服务,token 上下文不同:

| 类型 | 客户端 | 用途 | 路由依据 |
|---|---|---|---|
| **Site Agent Token** | quicktun-agent | rathole-client → rathole-server | token → site → project → `127.0.0.1:<minP>`(project 的 relay 端口段最小值) |
| **Operator Session Token** | quicktun CLI(`forward`) | operator → service relay 端口 | token → operator → 校验 CONNECT target 在 operator 可访问的 service relay 端口集合中 |

### Site Agent 路由

逻辑见 `internal/authproxy/router.go::Route`:

```go
func (r *Router) Route(ctx context.Context, rawToken string) (string, error) {
    siteID, err := dao.NewSiteAgentTokenDAO(r.db).ValidateRaw(ctx, rawToken)
    // ...
    var site model.Site
    r.db.First(&site, siteID)
    var project model.Project
    r.db.First(&project, site.ProjectID)
    if project.Status != model.ProjectStatusActive {
        return "", ErrUnauthenticated
    }
    minP, _, _ := resource.ParsePortRange(project.RelayPortRange)
    return fmt.Sprintf("127.0.0.1:%d", minP), nil
}
```

site agent 的 CONNECT target(URL host)字段被**忽略**,token 完全决定路由。

### Operator 路由

operator 走 `routeOperator`(查 sessions 表 → operator → CONNECT target 是 `127.0.0.1:<svcPort>`,要求 svcPort 属于 operator 有权限访问的 service)。

operator 的 CONNECT target 字段**起作用**,因为同一个 operator 可能 forward 多个 service。

## 部署形态

```
[互联网]
   │
   │ TCP/443 TLS
   ▼
[ nginx stream {} ]    deploy/nginx/quicktun-stream.conf
   │
   │ 127.0.0.1:8443    plain TCP after TLS termination
   ▼
[ quicktun-authproxy ]
   │
   │ 127.0.0.1:<port>  loopback
   ▼
[ rathole-server (per-project) ]
```

为啥要 nginx 在前?

- TLS 由 nginx + Let's Encrypt 处理,authproxy 自身不碰证书
- 同一个 :443 还能 SNI 多路复用(`stream { server_name RELAY_DOMAIN }`)
- 滚动升级时 nginx 可以不重启

## 共享 SQLite

auth-proxy 与控制面**共享同一个 SQLite 文件**,只读访问:

```yaml
# deploy/etc/authproxy.yaml.example
database:
  dsn: /var/lib/quicktun/quicktun.db?_foreign_keys=on&mode=ro
```

它读 `site_agent_tokens`、`sites`、`projects`、`sessions`、`services` 表完成 token 校验和路由。Phase 1 这样做是因为:

- 控制面与 authproxy 同机部署,无网络成本
- 简单(无新依赖)
- 写路径全在控制面,authproxy 不写表

Phase 2+ 如果要 HA,可以改为 gRPC 调控制面的 token 校验 API。

## /healthz

auth-proxy 默认在 `127.0.0.1:8444` 暴露 `/healthz`(可通过 `health_listen_addr` 改)。健康定义:DB ping 成功。

```bash
curl -s 127.0.0.1:8444/healthz
# {"status":"ok"}
```

systemd / launchd / k8s 可以拿这个做 liveness probe。

## 实现要点

- **CONNECT 解析用 `net/http.ReadRequest`**:不复用 `http.Server`,因为我们要 hijack 连接做 io.Copy
- **缓冲处理**:`bufio.Reader.Buffered()` 把 CONNECT 后被预读的字节 push 到 backend(rathole 协议头)
- **超时**:CONNECT 解析阶段 `SetReadDeadline(10s)`,转发后清掉
- **优雅关停**:监听 `ctx.Done()` 关 listener,等待 wg.Wait()

源码:`internal/authproxy/`(server.go ~150 行,router.go ~80 行)。
