---
sidebar_position: 3
---

# quicktun site

管理 site(跳板机)。

## 子命令

```
quicktun site list   <project>
quicktun site get    <name>
quicktun site create <project> <slug> [--display-name <name>] [--mode endpoint|subnet]
quicktun site delete <name> [--yes] [--force]
quicktun site get-install-command  <name>
quicktun site rotate-agent-token   <name>
```

## list

```bash
quicktun site list clinic-net
quicktun site list clinic-net --json
```

输出列:`NAME`、`DISPLAY`、`STATUS`、`MODE`、`LAST_SEEN`、`HOSTNAME`。

`STATUS` 可能值:`pending`(从未连过)、`online`、`offline`(超过 `site_offline_after` 没 heartbeat)。

## get

```bash
quicktun site get clinic-net/bastion-1
# 或完整名
quicktun site get projects/clinic-net/sites/bastion-1
```

## create

```bash
quicktun site create clinic-net bastion-1 \
    --display-name "Bastion 1"
```

| Flag | 必填 | 说明 |
|---|---|---|
| `--display-name` | | 给人看的名字 |
| `--mode` | | `endpoint`(默认)或 `subnet`。Phase 1 只支持 endpoint;subnet 字段预留给 Phase 2 NetBird |
| `--json` | | 输出 JSON |

## delete

```bash
quicktun site delete clinic-net/bastion-1
quicktun site delete clinic-net/bastion-1 --yes --force
```

`--force` 级联删 site 下的 service。

## get-install-command

颁发(或重新查看)site 的 install 命令,**raw token 只输出一次**:

```bash
quicktun site get-install-command clinic-net/bastion-1
```

输出:

```
Run on the bastion host:

  sudo ./deploy/install-agent.sh \
      --token a1b2c3d4...64hex \
      --control-endpoint control.example.com:443 \
      --auth-proxy relay.example.com:443
```

复制下来给 bastion operator。如果 token 已经发过,需要换新的用 `rotate-agent-token`。

## rotate-agent-token

颁发新 token,旧 token 立即失效:

```bash
quicktun site rotate-agent-token clinic-net/bastion-1
```

注意:这是个**断连操作**。旧 agent 会失去鉴权,需要 operator 用新 token 跑一次 install-agent.sh(脚本会重写 agent.yaml)。

## 资源命名

CLI 接受三段简写 `<project>/<site>` 和完整 `projects/<p>/sites/<s>` 两种形态。

## 看源码

`cmd/quicktun/cmd_site.go`
