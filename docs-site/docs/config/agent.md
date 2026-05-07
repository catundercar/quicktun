---
sidebar_position: 3
---

# agent.yaml

`/etc/quicktun/agent.yaml`(Linux)、`/etc/quicktun/agent.yaml`(macOS)、`C:\ProgramData\quicktun\agent.yaml`(Windows)。

源码:`internal/agent/config.go`。示例:`deploy/etc/agent.yaml.example`。

## 完整示例

```yaml
control_endpoint: control.example.com:443
token: a1b2c3d4...64hex
state_dir: /var/lib/quicktun-agent
rathole_binary: /usr/local/bin/rathole
rathole_args: ["--client"]
tls_insecure: false
hostname_override: ""
health_listen_addr: ""
```

## 字段

| Key | 类型 | 必填 | 默认 | 说明 |
|---|---|---|---|---|
| `control_endpoint` | string | ✅ | | 控制面 gRPC 地址,如 `control.example.com:443` |
| `token` | string | ✅ | | site agent **raw** token(从 `quicktun site get-install-command` 拿) |
| `state_dir` | string | | `/var/lib/quicktun-agent` | 渲染 `rathole-client.toml` 的目录,启动时如不存在则创建 |
| `rathole_binary` | string | | (空) | rathole 二进制路径。**空字符串 = render-only 模式**,不 spawn 子进程(测试用) |
| `rathole_args` | []string | | `["--client"]` | 跑 rathole 时的 args(配置文件路径会自动追加) |
| `tls_insecure` | bool | | `false` | 跳过 TLS 校验(dev 用) |
| `hostname_override` | string | | (空) | 强制 Bootstrap/Heartbeat 报告的 hostname。空 = `os.Hostname()` |
| `health_listen_addr` | string | | (空) | `/healthz` bind。空 = 禁用 |

## 关键点

### token 处理

agent 用 raw token 做两件事(详见 [Token 合约](../architecture/token-contract.md)):

1. **gRPC API**:`Authorization: Bearer <token>` 调控制面
2. **rathole-client.toml**:渲染时写 `token = "<sha256_hex(raw)>"`,与 rathole-server 配置匹配

token 文件 mode 必须 `0600`,owner 是 agent runtime 用户(Linux 的 `quicktun-agent`、macOS 的 root)。`install-agent.sh` 会处理这个。

### rathole_binary 空字符串

`rathole_binary: ""` 触发**render-only 模式**:

- agent 仍然 bootstrap、heartbeat、render `rathole-client.toml`
- 但**不会**起 rathole-client 子进程

用途:smoke 测试 / CI / 检查渲染输出。**生产环境不要这么配**(没有 supervisor 就没有反向隧道)。

注意:yaml.v3 无法区分 "key 不写" 和 "key 写空字符串",所以 `rathole_binary` 不写会导致默认 = "" = render-only。`install-agent.sh` 会显式写 `/usr/local/bin/rathole`。

### tls_insecure

只用于 dev:

- 控制面证书是自签
- 本地 docker compose / minikube 里跑

生产**永远 false**。

### state_dir

agent 启动时:

1. 如果不存在,创建(mode 0750)
2. 渲染 `<state_dir>/rathole-client.toml`(mode 0600)
3. supervisor exec rathole 时把这个路径作为最后一个 arg

systemd unit 的 `ReadWritePaths=/var/lib/quicktun-agent` 限制 agent 只能写这个路径。

### health_listen_addr

`agent.yaml` 配:

```yaml
health_listen_addr: 127.0.0.1:8445
```

启用 `/healthz`。健康定义:

- 最近一次 bootstrap < 2 × heartbeat_seconds 之前
- supervisor 子进程在跑(或 render-only 模式)

```bash
curl -s 127.0.0.1:8445/healthz
# {"status":"ok"}
# 或
# {"status":"degraded","reasons":["stale bootstrap"]}
```

## 验证规则

`validate()`:

- `control_endpoint` 必填
- `token` 必填

其他字段缺省都有合理 default。
