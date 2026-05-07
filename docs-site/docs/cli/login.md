---
sidebar_position: 1
---

# quicktun login

登录控制面,把会话 token 写到本地 credentials.yaml。

## 用法

```
quicktun login --endpoint <host:port> --email <email> [flags]
```

## Flags

| Flag | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--endpoint` | string | ✅ | 控制面 gRPC 地址,如 `control.example.com:443` |
| `--email` | string | ✅ | operator 邮箱 |
| `--password` | string | 二选一 | operator 密码(命令行明文) |
| `--password-stdin` | bool | 二选一 | 从 stdin 读密码;TTY 下隐藏输入,管道下读至 EOF |
| `--auth-proxy` | string | | auth-proxy 公网地址 `host:port`(用于后续 `quicktun forward`) |
| `--insecure` | bool | | 跳过 TLS 校验(仅 dev) |
| `--config` | string(persistent) | | credentials.yaml 路径,默认 `~/.config/quicktun/credentials.yaml` |

`--password` 和 `--password-stdin` 必选其一。

## 示例

### 交互式输入密码

```bash
quicktun login \
    --endpoint control.example.com:443 \
    --email admin@example.com \
    --password-stdin
# Password: (输入,不显示)
# Logged in as admin@example.com. Token saved to ~/.config/quicktun/credentials.yaml
```

### 管道喂密码

```bash
echo 'mysecret' | quicktun login \
    --endpoint control.example.com:443 \
    --email admin@example.com \
    --password-stdin
```

### 完整设置(含 auth-proxy)

```bash
quicktun login \
    --endpoint control.example.com:443 \
    --email admin@example.com \
    --auth-proxy relay.example.com:443 \
    --password-stdin
```

`--auth-proxy` 写进 credentials,`quicktun forward` 后续会用。

### Dev 环境(跳过 TLS)

```bash
quicktun login \
    --endpoint 127.0.0.1:9090 \
    --email admin@example.com \
    --password admin \
    --insecure
```

## credentials.yaml

成功登录后写入(默认 `~/.config/quicktun/credentials.yaml`,mode 0600):

```yaml
endpoint: control.example.com:443
operator_email: admin@example.com
session_token: eyJhbGciOi...
auth_proxy_endpoint: relay.example.com:443
tls_insecure: false
```

后续命令(`project list`、`forward` 等)读这个文件,无需重新登录。

## 错误

- `--endpoint and --email are required` → 缺必填 flag
- `--password (or --password-stdin) is required` → 没给密码
- `login: rpc error: code = Unauthenticated` → 邮箱或密码错
- `login: connection refused` → 控制面没起,或 endpoint 错
- `transport: authentication handshake failed: tls:` → TLS 证书问题,dev 加 `--insecure`,生产检查证书

## 看源码

`cmd/quicktun/cmd_login.go` `cmd/quicktun/dial.go`
