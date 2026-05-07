---
sidebar_position: 7
---

# 自监控

quicktun Phase 1 内置三个自监控功能:**site sweeper**、**`/healthz` 端点**、**`quicktun status` 总览**。无外部依赖,够小团队用。Prometheus / Grafana 集成留给 Phase 2。

## Site liveness sweeper

控制面里跑一个后台 goroutine,定期把"长时间没 heartbeat 但状态还是 online"的 site 翻成 offline。

### 行为

- 默认每 30 秒扫一次(`backend.sweeper_interval`)
- site 上次 heartbeat 超过 90 秒(`backend.site_offline_after`)就翻 offline
- 不动 `pending`(从未连过)的 site —— 留着方便 operator 肉眼分辨

### 配置

`/etc/quicktun/server.yaml`:

```yaml
backend:
  sweeper_interval: 30s
  site_offline_after: 90s
```

`0` 或空值 = 禁用 sweeper(仅测试用)。

### 为啥需要

agent 优雅停机会自己上报 offline,但**进程崩溃 / 网络中断**时 last_seen_time 卡住,site 一直 online,容易误导 operator。sweeper 兜底。

源码:`internal/sweeper/`。

## /healthz 端点

三个二进制各自有 `/healthz`,JSON 格式:

```json
// 200 OK
{"status": "ok"}

// 503 Service Unavailable
{"status": "degraded", "reasons": ["db ping failed"]}
```

### 控制面

复用 grpc-gateway 的 HTTP listener(`http_listen`),无需额外端口:

```bash
curl http://127.0.0.1:9091/healthz
```

健康定义:DB ping 成功。

### auth-proxy

独立的 HTTP listener,默认 `127.0.0.1:8444`(改 `health_listen_addr`):

```bash
curl http://127.0.0.1:8444/healthz
```

健康定义:DB ping 成功(读权限即可)。

### agent

**默认禁用**(`health_listen_addr: ""`)。要启用:

```yaml
# /etc/quicktun/agent.yaml
health_listen_addr: 127.0.0.1:8445
```

健康定义:

- 最近一次 bootstrap < 2 × heartbeat_seconds 之前
- supervisor 子进程在跑(或 render-only 模式)

### 用途

systemd / launchd / k8s liveness probe 都能直接用:

```ini
# systemd
ExecStartPost=/bin/sh -c 'until curl -s 127.0.0.1:8445/healthz >/dev/null; do sleep 1; done'
```

```yaml
# k8s
livenessProbe:
  httpGet:
    path: /healthz
    port: 8445
```

源码:`internal/health/`。

## quicktun status

admin operator 用 CLI 看实时总览:

```bash
$ quicktun status
operators:        3
projects:         5 active, 1 disabled
sites:           12 online, 3 offline, 1 pending
services:        37
supervisors:      5

stale sites (no recent heartbeat):
  projects/clinic-net/sites/bastion-3   last_seen=2026-05-07T10:23:14Z  hostname=bastion-3.lan
```

`stale sites`:status=online 但最近 30 秒没 heartbeat —— 即将被 sweeper 切 offline,可以提前看到。

详细字段说明见 [CLI / status](./cli/status.md)。

## 监控集成 pattern

### 简单 cron 报警

```bash
# /etc/cron.d/quicktun-monitor
*/5 * * * * quicktun /usr/local/bin/check-quicktun.sh
```

`check-quicktun.sh`:

```bash
#!/bin/bash
set -euo pipefail

STATUS=$(quicktun status --json)
OFFLINE=$(echo "$STATUS" | jq -r '.siteCountOffline // 0')
STALE=$(echo "$STATUS" | jq -r '.staleSites | length')

if [ "$OFFLINE" -gt 0 ] || [ "$STALE" -gt 0 ]; then
    msg="quicktun: offline=$OFFLINE stale=$STALE"
    # 发到你的告警通道
    curl -X POST https://hooks.slack.com/services/... -d "{\"text\":\"$msg\"}"
fi
```

### k8s

把 deployment 的 `livenessProbe` 配 `/healthz`,kubelet 自动重启不健康的 pod。

### Prometheus(Phase 2)

留给 Plan 12。届时三个二进制会暴露 `/metrics`(请求计数 / latencies / supervisor restart),配标准的 Grafana dashboard。

## 看源码

- `internal/sweeper/sweeper.go` — sweeper goroutine
- `internal/health/health.go` — `/healthz` http handler
- `internal/grpcsvc/admin_service.go` — `GetSystemStatus` RPC
- `cmd/quicktun/cmd_status.go` — CLI 渲染
