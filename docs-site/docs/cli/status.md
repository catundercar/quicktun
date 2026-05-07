---
sidebar_position: 6
---

# quicktun status

管理员视角的控制面运行态总览。**仅 admin operator 可用**。

## 用法

```
quicktun status [--json]
```

## 输出

```
$ quicktun status
operators:        3
projects:         5 active, 1 disabled
sites:           12 online, 3 offline, 1 pending
services:        37
supervisors:      5

stale sites (no recent heartbeat):
  projects/clinic-net/sites/bastion-3   last_seen=2026-05-07T10:23:14Z  hostname=bastion-3.lan
```

字段含义:

| 字段 | 含义 |
|---|---|
| `operators` | 注册的 operator 总数 |
| `projects` | active(`status='active'`)/ disabled(`status='disabled'`)的 project 计数 |
| `sites` | online / offline / pending 的 site 计数 |
| `services` | service 总数 |
| `supervisors` | 当前在跑的 rathole-server 子进程数(每个 active project 一个) |
| `stale sites` | `status='online'` 但最近 30 秒没 heartbeat 的 site —— 即将被 sweeper 标 offline |

## --json

```bash
quicktun status --json
```

输出 protobuf JSON,字段对应 `GetSystemStatusResponse`:

```json
{
  "operatorCount": 3,
  "projectCountActive": 5,
  "projectCountDisabled": 1,
  "siteCountOnline": 12,
  "siteCountOffline": 3,
  "siteCountPending": 1,
  "serviceCount": 37,
  "supervisorRunningCount": 5,
  "now": "2026-05-07T10:24:00Z",
  "staleSites": [
    {
      "name": "projects/clinic-net/sites/bastion-3",
      "lastSeenAt": "2026-05-07T10:23:14Z",
      "status": "online",
      "hostname": "bastion-3.lan"
    }
  ]
}
```

适合给监控脚本 / 运维 dashboard 拉。

## 权限

只有 `is_admin=true` 的 operator 能调。普通 operator 调会拿到:

```
status: rpc error: code = PermissionDenied desc = admin only
```

要新建 admin operator(在控制面机上跑):

```bash
sudo -u quicktun /usr/local/bin/quicktun-server admin create-operator \
    --config /etc/quicktun/server.yaml \
    --email admin2@example.com --password '...' --admin
```

## 用途

- **健康监控**:cron 每 5 分钟跑 `status --json`,异常时报警
- **容量规划**:看 supervisors 数 / sites 数,评估当前 VPS 资源够不够
- **stale sites 报警**:有 stale site 但没切到 offline → agent 心跳卡住,要去看 site 的 journalctl

## 看源码

`cmd/quicktun/cmd_status.go`(CLI 端)+ `internal/grpcsvc/admin_service.go`(server 端)
