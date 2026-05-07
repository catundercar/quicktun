---
sidebar_position: 2
---

# authproxy.yaml

`/etc/quicktun/authproxy.yaml`。源码:`internal/authproxy/config.go`。示例:`deploy/etc/authproxy.yaml.example`。

## 完整示例

```yaml
listen_addr: 127.0.0.1:8443

health_listen_addr: 127.0.0.1:8444

database:
  dsn: /var/lib/quicktun/quicktun.db?_foreign_keys=on&mode=ro

log:
  level: info
```

## 字段

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `listen_addr` | string | `:8443` | auth-proxy 绑的地址。生产推荐 `127.0.0.1:8443`,nginx stream {} 反代 :443 |
| `health_listen_addr` | string | `127.0.0.1:8444` | `/healthz` HTTP bind。空字符串表示**仍然 default 到 loopback**(yaml.v3 无法区分 omit / 显式空,操作员要禁用需 wrapper) |
| `database.dsn` | string | (必填) | 控制面同一个 SQLite 文件,**只读**(`mode=ro`) |
| `log.level` | string | `info` | `debug` / `info` / `warn` / `error` |

## 关键点

### 共享 DB

auth-proxy 与控制面**同机部署**,共享同一个 `quicktun.db` 文件,**只读**访问。这样:

- 没有网络成本(loopback file IO)
- 没有新依赖(不要 gRPC token 校验 API)
- 写路径全在控制面,auth-proxy 永远不会污染数据

DSN 必须加 `mode=ro`(否则 SQLite 会启用 WAL 写,与控制面冲突)。

### /healthz

`HealthListenAddr` 默认绑 `127.0.0.1:8444`,**不暴露公网**。systemd / launchd / k8s 用 loopback probe:

```bash
curl -s 127.0.0.1:8444/healthz
# {"status":"ok"}    或 {"status":"degraded","reasons":[...]}
```

健康定义:`db.Ping()` 成功(SQLite 文件可读)。

如果要从外部 monitor 探,改成 `0.0.0.0:8444` 或加 nginx 反代 `/healthz` location。

### listen_addr 的安全语义

`listen_addr: 127.0.0.1:8443` **必须**绑 loopback:

- auth-proxy 自己**不做 TLS**,plain HTTP CONNECT
- 直接把 `:8443` 暴露公网会让任何人发未加密的 CONNECT 请求(虽然没 token 也过不了认证,但不优雅)
- 生产架构:nginx 终结 TLS,反代到 auth-proxy 的 loopback

dev 调试时可以 `0.0.0.0:8443` 直接打,但**不要**这么部署到生产。

## 验证规则

`validate()`:

- `database.dsn` 必填

其他字段都有 default,缺了不报错。
