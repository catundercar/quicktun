---
sidebar_position: 7
---

# 自监控

quicktun 内置五个自监控功能:**site sweeper**、**`/healthz` 端点**、**`quicktun status` 总览**、**Prometheus `/metrics` 指标**、**webhook 告警**。前三项无外部依赖,够小团队用;后两项面向接 Prometheus / Grafana / Alertmanager 的环境。

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

### Prometheus

见下面的"Prometheus 指标"章节。三个二进制都已暴露 `/metrics`,可以直接接 prometheus + Grafana。

## Prometheus 指标

三个 binary 各自暴露 Prometheus 文本格式的 `/metrics`。控制面把 `/metrics` 挂在 grpc-gateway 的 HTTP listener(`http_listen`),所以一个 nginx upstream 就能同时抓 `/v1/*` + `/healthz` + `/metrics`。auth-proxy 和 agent 默认不开 metrics listener,需要在 YAML 里显式打开 `metrics_listen_addr`(通常 `127.0.0.1:8445` 之类的回环地址)。

### 控制面(quicktun-server)

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `quicktun_server_requests_total` | counter | `method`, `code` | gRPC 请求次数,按 method + 状态码切分。`code="Unauthenticated"` 突增 → token 暴破信号。 |
| `quicktun_server_request_latency_seconds` | histogram | `method` | 请求耗时(默认 buckets)。|
| `quicktun_server_sweeper_flipped_total` | counter | — | sweeper 把 online site 翻成 offline 的累计次数。|
| `quicktun_server_supervisor_restarts_total` | counter | `project_id` | 每个项目的 rathole-server 子进程退出次数(包括优雅停机)。持续上涨 = 配置 / 网络故障。|
| `quicktun_server_supervisor_alive` | gauge | `project_id` | 1 = 当前 rathole-server 在跑;0 = 没在跑。配 alert 用。|

抓取:

```yaml
# prometheus.yml
- job_name: quicktun-server
  scrape_interval: 30s
  static_configs:
    - targets: ['control.example.com:9091']
```

### auth-proxy

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `quicktun_authproxy_connect_total` | counter | `status` | CONNECT 请求按 HTTP 状态码统计(200 / 401 / 405 / 500 / 502)。|
| `quicktun_authproxy_connect_latency_seconds` | histogram | `status` | CONNECT 收到 → 状态行写出耗时。|

启用:

```yaml
# /etc/quicktun/authproxy.yaml
metrics_listen_addr: 127.0.0.1:8445
```

### agent

| 指标 | 类型 | 标签 | 含义 |
|------|------|------|------|
| `quicktun_agent_bootstrap_total` | counter | `result=ok\|err` | Bootstrap 调用次数。`err` 持续上涨 = 控制面不可达 / token 失效。|
| `quicktun_agent_heartbeat_total` | counter | `result` | 同上,针对 Heartbeat。|
| `quicktun_agent_supervisor_alive` | gauge | — | 1 = rathole-client 子进程在跑。|

启用:

```yaml
# /etc/quicktun/agent.yaml
metrics_listen_addr: 127.0.0.1:8446
```

### Grafana Dashboard 草图

四个核心 panel:

1. **Request rate by method**:`sum by (method) (rate(quicktun_server_requests_total[5m]))`
2. **401 突发率**:`rate(quicktun_server_requests_total{code="Unauthenticated"}[5m])`
3. **Supervisor 健康度**:`sum by (project_id) (quicktun_server_supervisor_alive)` — 任何项目长期 = 0 即告警。
4. **Crash rate**:`rate(quicktun_server_supervisor_restarts_total[5m])` — 高频 = 配置错误或上游崩。

完整 JSON dashboard 待社区贡献(欢迎 PR)。

## webhook 告警

控制面在 supervisor 进入 crash-loop 时 POST 一个 JSON 到 `backend.webhook_url`。

### 触发条件

任意一个项目的 rathole-server 在 `crash_loop_window`(默认 5 分钟)内退出超过 `crash_loop_threshold`(默认 5)次。一旦触发,该项目的窗口立即重置,直到下一个完整阈值才会再发 —— 防 webhook 风暴。

### 配置

```yaml
# /etc/quicktun/server.yaml
backend:
  webhook_url: "https://hooks.example.com/quicktun"
  webhook_timeout: 5s
  crash_loop_threshold: 5
  crash_loop_window: 5m
```

`webhook_url` 留空 = 完全禁用告警(不报错)。

### Payload 示例

```json
{
  "type": "supervisor_crash_loop",
  "subject": "project=42",
  "message": "rathole-server for project 42 crashed 5 times in 5m0s",
  "time": "2026-05-07T10:23:14.123456Z",
  "extra": {
    "project_id": "42",
    "crash_count": 5,
    "window_seconds": 300
  }
}
```

接收端可以是 Slack incoming webhook、Discord webhook、Alertmanager webhook receiver,或者你自己的 ops bot。

### 接 Slack 的最小例子

Slack 只接 `{"text": "..."}`。最方便的做法是在 Slack 和 quicktun 之间挂一个简单的转换 endpoint(nginx + lua / 一个 5 行 Python script):读 quicktun 的 `message` 字段,塞进 `text`,转发到 Slack 的 hook。

源码:`internal/notify/`、`internal/metrics/`。

## 看源码

- `internal/sweeper/sweeper.go` — sweeper goroutine
- `internal/health/health.go` — `/healthz` http handler
- `internal/metrics/` — Prometheus 注册 + collector 定义
- `internal/notify/webhook.go` — webhook POST
- `internal/notify/crashloop.go` — crash-loop 检测
- `internal/grpcsvc/admin_service.go` — `GetSystemStatus` RPC
- `cmd/quicktun/cmd_status.go` — CLI 渲染
