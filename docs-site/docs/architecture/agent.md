---
sidebar_position: 3
---

# agent 内部

`quicktun-agent` 是跳板机上的常驻进程。它有 5 个内部模块:**bootstrap / heartbeat / render / supervisor / bridge**。

## 状态机

```
        ┌──────────────────────────────────────────────────┐
        │                  quicktun-agent                  │
        └──────────────────────────────────────────────────┘
              │
              │ 1. Load /etc/quicktun/agent.yaml
              ▼
        ┌──────────────┐
        │  bootstrap   │── gRPC Bootstrap(Bearer raw) ──► control-plane
        │              │   ◄── BootstrapResponse(tunnels,
        │              │       auth_proxy_endpoint, config_version)
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐
        │   bridge     │ listen 127.0.0.1:0 (auth-proxy CONNECT bridge)
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐
        │   render     │ write <state_dir>/rathole-client.toml
        │              │   - remote_addr = 127.0.0.1:<bridge_port>
        │              │   - token = sha256_hex(raw)
        │              │   - per-service [client.services.<key>]
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐
        │  supervisor  │ exec rathole <args> <toml>
        │              │ KeepAlive + back-off restart
        └──────┬───────┘
               │
               ▼
        ┌──────────────┐
        │  heartbeat   │ every 15s gRPC Heartbeat → 更新 last_seen
        │              │ if config_version 漂移 → 重 bootstrap
        └──────────────┘
```

## bootstrap

入口:`internal/agent/runtime.go::Run` 第一步。客户端代码 `internal/agent/bootstrap.go`(简单 gRPC client wrapper)。

请求:

```protobuf
message BootstrapRequest {
  string hostname = 1;
  string os = 2;
  string agent_version = 3;
}
```

响应:

```protobuf
message BootstrapResponse {
  string site_name = 1;        // projects/p/sites/s
  string project_slug = 2;
  string site_slug = 3;
  string auth_proxy_endpoint = 4;  // relay.example.com:443
  repeated TunnelBinding tunnels = 5;
  int32 heartbeat_seconds = 6;
  string config_version = 7;
}

message TunnelBinding {
  string service_slug = 1;
  string target_addr = 2;
  uint32 target_port = 3;
  string proto = 4;
  uint32 relay_port = 5;
}
```

启动时如果 bootstrap 失败,exponential backoff 重试。

## bridge(关键)

`bridge` 是一个**进程内 in-memory listener**,绑在 `127.0.0.1:<random>`。它的唯一目的:让 rathole-client 把 quicktun 的 CONNECT + Bearer 协议透明化掉。

工作流:

```
rathole-client      bridge (127.0.0.1:R)        auth-proxy
     │                       │                       │
     │ TCP connect            │                       │
     │──────────────────────►│                       │
     │                       │ TCP connect            │
     │                       │ → cfg.AuthProxyEndpoint│
     │                       │──────────────────────►│
     │                       │ CONNECT relay:443      │
     │                       │ Bearer <raw_token>    │
     │                       │──────────────────────►│
     │                       │ ◄── HTTP/1.1 200 OK ──│
     │ ◄═══════════ bidirectional io.Copy ════════════►
```

代码:`internal/agent/bridge.go`,~80 行。每个连接一个 goroutine 处理。

为啥要 bridge?因为 rathole 协议本身不知道 HTTP CONNECT。我们想让 rathole 走 :443(共用一个公网端口、TLS 终结),所以 quicktun 在中间插一层 CONNECT 网关。bridge 是 agent 端的客户端实现。

## render

`internal/agent/render.go::RenderRatholeClient`。从 `BootstrapResponse` 生成 rathole-client TOML:

```toml
# quicktun-agent rendered config for projects/clinic-net/sites/bastion-1
# DO NOT EDIT MANUALLY.

[client]
remote_addr = "127.0.0.1:54321"   # bridge 的本地端口

[client.services.bastion-1__ssh]
token = "<sha256_hex(raw_token)>"
local_addr = "127.0.0.1:22"

[client.services.bastion-1__rdp]
token = "<sha256_hex(raw_token)>"
local_addr = "192.168.10.50:3389"
```

注意:

- `remote_addr` 指向 **bridge**,不是 auth-proxy。rathole 进程不知道 quicktun 的存在
- `token` 是 `sha256_hex(raw)`,与 control plane 给 rathole-server 渲染的 token 字段一致
- service 键名是 `<site_slug>__<service_slug>`,server 端 `internal/relay/render.go` 用同样规则

## supervisor

`internal/supervisor/`(独立包,server / agent 共用)。负责:

- `os/exec` spawn 子进程,捕获 stdout/stderr 包装成 zap log
- 进程退出 → exponential backoff 重启
- ctx 取消 → SIGTERM,5 秒后还没退则 SIGKILL
- 状态查询 `Pid()`、`Restarts()`

agent 用法:

```go
// internal/agent/runtime.go
sup := supervisor.New(supervisor.Config{
    Binary: r.cfg.RatholeBinary,
    Args:   append(r.cfg.RatholeArgs, ratholeTOMLPath),
    Logger: r.lg,
})
go sup.Run(ctx)
```

如果 `cfg.RatholeBinary == ""`,supervisor 不启动(render-only 模式,smoke 测试用)。

## heartbeat

每 `HeartbeatSeconds` 秒(默认 15s)发一次:

```protobuf
message HeartbeatRequest {
  string hostname = 1;
  string os = 2;
  string agent_version = 3;
  repeated string lan_cidrs = 4;
  string config_version = 5;   // agent 当前在跑的版本
}

message HeartbeatResponse {
  bool should_rebootstrap = 1;
  google.protobuf.Timestamp server_time = 2;
}
```

server 端逻辑:

- 更新 `Site.last_seen_time / hostname / os / agent_version`
- 如果 `req.config_version != computeConfigVersion(svcs)` → 返回 `should_rebootstrap=true`

agent 收到 `should_rebootstrap=true`:

1. 重 bootstrap 拿新的 BootstrapResponse
2. 重新 render `rathole-client.toml`
3. 通过 supervisor 重启 rathole 子进程(写文件 + restart)

## /healthz(可选)

`agent.yaml` 配 `health_listen_addr: 127.0.0.1:8445` 后,agent 起一个 HTTP 服务暴露 `/healthz`。健康定义:

- 最近一次 bootstrap < 2 × heartbeat_interval 之前
- supervisor 子进程跑着(或 `RatholeBinary == ""` 的 render-only 模式)

源码:`internal/agent/runtime.go` health 部分。

## 配置

完整字段说明见 [配置参考 / agent](../config/agent.md)。最小配置:

```yaml
control_endpoint: control.example.com:443
token: <RAW_TOKEN>
state_dir: /var/lib/quicktun-agent
rathole_binary: /usr/local/bin/rathole
rathole_args: ["--client"]
```
