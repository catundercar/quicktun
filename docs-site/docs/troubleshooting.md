---
sidebar_position: 8
---

# 常见问题排查

按"症状 → 排查思路"排列。

## Agent

### Agent 起不来,journalctl 看到 `config: token is required`

`/etc/quicktun/agent.yaml` 里 `token:` 字段为空或缺失。重跑 install-agent.sh 或手动编辑文件,然后:

```bash
sudo systemctl restart quicktun-agent
```

### Agent 起来后立刻 exit,日志只有 `bootstrap: rpc error`

可能原因:

| code | 含义 | 排查 |
|---|---|---|
| `Unauthenticated` | token 错或被 rotate 过 | 重新颁发:`quicktun site rotate-agent-token <site>`,然后重跑 install-agent.sh |
| `connection refused` | control_endpoint 错或控制面没起 | `nc -zv control.example.com 443` |
| `transport: ... TLS handshake error` | 证书问题 | dev 加 `tls_insecure: true`;生产检查证书 SAN 包不包 `control.example.com` |

### Agent 拿不到 bootstrap,死循环

通常是 token 失效但 agent 还没意识到,exponential backoff 在重试。停服务,看清楚 token 状态:

```bash
sudo systemctl stop quicktun-agent
sudo journalctl -u quicktun-agent -n 50

# 在 operator 工作站验证 token
quicktun site get-install-command <site>   # 拿一个新的
```

### Agent log 里 `agent bridge: dial auth-proxy ... connection refused`

auth-proxy 没起,或 nginx stream {} 没生效。

```bash
# 控制面机器
sudo systemctl status quicktun-authproxy
ss -ltnp | grep 8443    # authproxy 应该在
ss -ltnp | grep 443     # nginx 应该在

# 从 agent 机器
curl -v https://relay.example.com:443/   # 至少要握手成功
```

### Agent log 里 `auth-proxy rejected (401)`

token 有效但 auth-proxy 拒了。可能性:

1. **数据库 race**:control plane 刚 rotate 了 token,但 auth-proxy 的 SQLite read snapshot 还是旧的(SQLite WAL 的边界情况)。等 30 秒再看。
2. **project disabled**:运维操作把 project 标 `disabled` → auth-proxy 拒所有这个 project 的 token。
3. **token expire**:`expire_time` 字段过期。Phase 1 默认不设 expire,排除。

排查:

```bash
sqlite3 /var/lib/quicktun/quicktun.db "SELECT site_id, token_hash, expire_time FROM site_agent_tokens;"
sqlite3 /var/lib/quicktun/quicktun.db "SELECT id, slug, status FROM projects;"
```

### supervisor 一直在重启 rathole

```
supervisor: child exited code=1, restart in 5s
supervisor: child exited code=1, restart in 5s
```

原因:

- `rathole_binary` 路径错或没执行权限 → `ls -la /usr/local/bin/rathole`
- rathole-client.toml 渲染有问题 → `cat /var/lib/quicktun-agent/rathole-client.toml`,手动跑 `rathole --client /var/lib/quicktun-agent/rathole-client.toml` 看真正的错
- 端口被占 / 网络问题(rathole 自己 log 会说)

## Operator / forward

### `quicktun forward` 报 `credentials missing auth_proxy_endpoint`

`quicktun login` 时没配 `--auth-proxy`。重新登录:

```bash
quicktun login --endpoint control.example.com:443 \
    --email admin@example.com \
    --auth-proxy relay.example.com:443 \
    --password-stdin
```

### `quicktun forward` 报 `auth-proxy rejected (401)`

session token 过期或被 logout。重新 login。

### `quicktun forward` 启动 OK 但 ssh 连不上

```
Forwarding 127.0.0.1:2222 -> projects/.../ssh (auth-proxy: relay.example.com:443)
ssh: connect to host 127.0.0.1 port 2222: Connection reset by peer
```

排查链:

1. **agent 在线吗?** `quicktun site get <site>` 看 `last_seen_time`,< 30 秒前才算在线
2. **service 配的 target 对吗?** `quicktun service get <name>` 确认 `target_addr / target_port`
3. **跳板机 reachable 吗?** ssh 进跳板机手动 `nc -zv <target_addr> <target_port>`
4. **rathole tunnel 起了吗?** 跳板机 `journalctl -u quicktun-agent -n 20`,应该有 `tunnel established`

### service create 后 forward 还是 503 / 502

agent 还没 heartbeat 到新的 `config_version`,等 ~30 秒(默认 heartbeat 15s + 重 bootstrap + rathole-client 重启)。

强制立即生效:`sudo systemctl restart quicktun-agent`(在跳板机上)。

## 控制面

### `quicktun-server` 启动失败,journalctl 报 `config: ... is required`

server.yaml 缺必填字段。对照 [配置参考 / server](./config/server.md) 检查:

- `database.driver` / `database.dsn`
- `control_plane.grpc_listen`
- `session.default_ttl`

### `quicktun login` 返回 `transport: ... TLS handshake`

nginx 没正确反代 grpc。

```bash
# 控制面机
sudo nginx -t
sudo systemctl status nginx
ss -ltnp | grep 443    # nginx 应该 listen

# 在 nginx 端临时绕过:
quicktun login --endpoint 127.0.0.1:9090 --insecure ...
# 如果这个能用,就是 nginx 配的问题
```

详细 nginx 配置见 [部署 / nginx](./deployment/nginx.md)。

### `quicktun project create` 报 `relay_port_range overlaps with project ...`

quicktun 拒绝端口段重叠的 project,因为 rathole 进程会冲突。换一个不重叠的:

```bash
quicktun project create new-project --port-range 21000-21099
```

已用的端口段:`quicktun project list` 看 `PORT_RANGE` 列。

### auth-proxy 报 `no such file or directory: quicktun.db`

DB 路径配错了,或控制面机已经卸载。检查:

```yaml
# /etc/quicktun/authproxy.yaml
database:
  dsn: /var/lib/quicktun/quicktun.db?_foreign_keys=on&mode=ro
```

确保 `/var/lib/quicktun/quicktun.db` 存在且 auth-proxy user(默认 `quicktun`)能读。

## smoke 测试

### `./scripts/smoke.sh` 失败

跑出来的 log 通常已经指明哪一步:

- `migrate failed` → DB 路径或 schema 问题,检查 `migrations/`
- `login failed` → admin user 没创建,跑 `admin create-operator`
- `forward failed` → 上面 forward 章节的所有问题都可能

可以手动跑步骤:

```bash
./bin/quicktun-server serve --config etc/server.yaml &
./bin/quicktun-authproxy run --config etc/authproxy.yaml &
./bin/quicktun login --endpoint 127.0.0.1:9090 --email admin@... --insecure
# ...
```

### `./scripts/smoke-agent.sh` 在 render 之后 stuck

agent 起来后 heartbeat loop 永久 retry。检查:

- 控制面在跑吗?
- token 有效吗?

通常重新生成 token 重跑。

## 求助

- [GitHub Issues](https://github.com/catundercar/quicktun/issues)
- 仓库内开发者文档:`docs/00-overview.md` ~ `docs/08-roadmap.md`
- 设计 plan:`docs/superpowers/plans/`
