---
sidebar_position: 1
---

# server.yaml(控制面)

`/etc/quicktun/server.yaml`。源码:`internal/config/config.go`。示例:`deploy/etc/server.yaml.example`。

## 完整示例

```yaml
control_plane:
  grpc_listen: 127.0.0.1:9090
  http_listen: 127.0.0.1:9091
  relay_addr: relay.example.com

database:
  driver: sqlite
  dsn: /var/lib/quicktun/quicktun.db?_foreign_keys=on

session:
  default_ttl: 24h

log:
  level: info
  path: ""
  max_size_mb: 100
  max_age_days: 30
  max_backups: 7

backend:
  rathole_binary: /usr/local/bin/rathole
  rathole_args: ["--server"]
  rathole_config_dir: /var/lib/quicktun/relays
  auth_proxy_public_addr: relay.example.com:443
  sweeper_interval: 30s
  site_offline_after: 90s
```

## control_plane

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `grpc_listen` | string | `0.0.0.0:9443` | gRPC bind。生产建议 `127.0.0.1:9090`,nginx 反代 `:443` 上去 |
| `http_listen` | string | `0.0.0.0:9080` | grpc-gateway HTTP/JSON bind。生产建议 `127.0.0.1:9091` |
| `relay_addr` | string | `relay.example.com:443` | 公网 relay 地址(写到 `BootstrapResponse.AuthProxyEndpoint` 给 agent)。当 `backend.auth_proxy_public_addr` 为空时用这个回退 |

## database

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `driver` | string | `sqlite` | Phase 1 只支持 sqlite |
| `dsn` | string | (必填) | SQLite DSN,推荐加 `?_foreign_keys=on` |

## session

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `default_ttl` | duration | `8h` | operator login bearer token 默认有效期 |

duration 用 Go 时间格式:`1s` `30m` `8h` `24h`。

## log

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `level` | string | `info` | `debug` / `info` / `warn` / `error` |
| `path` | string | `""` | 空 = stderr(systemd 收 → journald);设路径则启用 lumberjack 文件轮换 |
| `max_size_mb` | int | `100` | lumberjack 单文件大小阈值 |
| `max_age_days` | int | `30` | 旧日志保留天数 |
| `max_backups` | int | `7` | 保留多少个轮换的旧文件 |

## backend

| Key | 类型 | 默认 | 说明 |
|---|---|---|---|
| `rathole_binary` | string | `rathole` | rathole 二进制路径(或 PATH 中的名字) |
| `rathole_args` | []string | `["--server"]` | 跑 rathole 时的 args(不含配置文件路径,supervisor 会附加) |
| `rathole_config_dir` | string | `/var/lib/quicktun/relays` | 控制面渲染 rathole-server-`<project>`.toml 文件的目录 |
| `auth_proxy_public_addr` | string | `""` | agent 看到的 auth-proxy 公网地址。空 = 回退到 `control_plane.relay_addr`(legacy / dev) |
| `sweeper_interval` | duration | `30s` | site liveness sweeper 跑的间隔。0 = 禁用 sweeper |
| `site_offline_after` | duration | `90s` | site 多久没 heartbeat 算 offline。0 = 禁用 |

## 验证规则

`Validate()`(`internal/config/config.go`):

- `database.driver` 必填且必须是 `sqlite`(Phase 1)
- `database.dsn` 必填
- `control_plane.grpc_listen` 必填
- `session.default_ttl` 必须 > 0

启动失败时会在日志看到 `config: ... is required` 之类的错误。
