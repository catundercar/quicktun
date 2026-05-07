---
sidebar_position: 2
---

# quicktun project

管理 project(数据 + relay 进程隔离边界)。

## 子命令

```
quicktun project list
quicktun project get    <slug>
quicktun project create <slug> --port-range <range> [--display-name <name>]
quicktun project delete <slug> [--yes] [--force]
```

## list

```bash
quicktun project list
quicktun project list --json
```

输出列:`NAME`、`DISPLAY`、`STATUS`、`PORT_RANGE`。

## get

```bash
quicktun project get clinic-net
# 或完整名
quicktun project get projects/clinic-net
```

## create

```bash
quicktun project create clinic-net \
    --display-name "Clinic Network" \
    --port-range 20000-20099
```

| Flag | 必填 | 说明 |
|---|---|---|
| `--port-range` | ✅ | rathole-server 用的端口段(每个 service 占一个端口),如 `20000-20099` |
| `--display-name` | | 给人看的名字 |
| `--json` | | 输出 JSON 而不是表格 |

**关键约束**:不同 project 的 `--port-range` **不能重叠**。控制面会拒绝重叠的请求。

创建成功后,控制面 supervisor 自动起一个 `rathole-server` 子进程,绑这个 project 的端口段。

## delete

```bash
# 默认会问 y/N,且如果 project 下还有 site 会拒绝
quicktun project delete clinic-net

# 跳过确认
quicktun project delete clinic-net --yes

# 强制级联删 site / service
quicktun project delete clinic-net --yes --force
```

删 project 会:

1. 拒绝(默认)或级联删(`--force`)其下的 site / service / token
2. 停掉对应的 rathole-server 进程
3. 释放端口段

## 资源命名

CLI 接受简写 `<slug>` 和完整 `projects/<slug>` 两种形态,内部都规范化为完整形态。

## 看源码

`cmd/quicktun/cmd_project.go`
