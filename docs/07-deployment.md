# 部署

## 1. quicktun-server 部署

### 1.1 硬件最低要求

| 资源 | Phase 1（10 site 内） | 100 site | 1000 site |
|---|---|---|---|
| vCPU | 2 | 4 | 8 |
| RAM | 2 GB | 4 GB | 16 GB |
| 网络 | 公网 IP；带宽视客户访问量 | 同 | 同 |
| 磁盘 | 10 GB SSD | 50 GB | 200 GB |

### 1.2 端口

| 端口 | 暴露范围 | 用途 |
|---|---|---|
| 443 | 公网 | quicktun-auth-proxy（agent + operator） |
| 9443 | 公网 | gRPC（控制面 API） |
| 9080 | 公网 | grpc-gateway HTTP/JSON |
| 22 | SSH 管理白名单 | 服务器自身管理（不属于 quicktun） |
| 20000-30000 | **仅 127.0.0.1** | rathole project 端口段 |

防火墙：

```bash
ufw default deny incoming
ufw allow 443/tcp
ufw allow 9443/tcp
ufw allow 9080/tcp
ufw allow from <your-mgmt-ip> to any port 22
ufw enable
```

### 1.3 目录结构

```
/opt/quicktun/
├── bin/
│   ├── quicktun-server          # 控制面
│   ├── quicktun-auth-proxy      # 准入代理
│   └── rathole                  # 反向代理（vendored）
├── etc/
│   ├── server.yaml              # 控制面配置
│   ├── tls/
│   │   ├── server.crt
│   │   └── server.key
│   └── relays/
│       ├── clinic-network.toml  # 控制面渲染
│       └── factory-iot.toml
└── var/
    ├── quicktun.db              # SQLite
    ├── log/
    │   ├── control-plane.log
    │   └── auth-proxy.log
    └── backup/
```

### 1.4 server.yaml

```yaml
control_plane:
  grpc_listen: 0.0.0.0:9443
  http_listen: 0.0.0.0:9080
  tls:
    cert: /opt/quicktun/etc/tls/server.crt
    key:  /opt/quicktun/etc/tls/server.key

database:
  driver: sqlite
  dsn: /opt/quicktun/var/quicktun.db?_journal_mode=WAL&_busy_timeout=5000

auth_proxy:
  listen: 0.0.0.0:443
  tls:
    cert: /opt/quicktun/etc/tls/server.crt
    key:  /opt/quicktun/etc/tls/server.key

backend:
  rathole:
    binary: /opt/quicktun/bin/rathole
    config_dir: /opt/quicktun/etc/relays

session:
  default_ttl: 8h

log:
  path: /opt/quicktun/var/log/control-plane.log
  level: info
  max_size_mb: 100
  max_age_days: 30
  max_backups: 7
```

### 1.5 systemd 单元

`/etc/systemd/system/quicktun-server.service`：

```ini
[Unit]
Description=quicktun control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=quicktun
Group=quicktun
ExecStart=/opt/quicktun/bin/quicktun-server --config /opt/quicktun/etc/server.yaml
Restart=always
RestartSec=2
LimitNOFILE=65536

# 安全 hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/quicktun/var /opt/quicktun/etc/relays
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true

# 资源限额
MemoryMax=4G
TasksMax=4096

[Install]
WantedBy=multi-user.target
```

注意：control plane **自身**不是 root，但子进程（rathole / auth-proxy）也跑在同一个 quicktun 用户下。如果想进一步隔离，可以为子进程配单独 user，supervisor 用 `Credential` 切换。

### 1.6 一键安装脚本

```bash
#!/bin/bash
# install-server.sh
set -euo pipefail

# 1. 创建用户
useradd -r -s /usr/sbin/nologin quicktun || true

# 2. 拉二进制 + rathole
curl -fsSL https://github.com/<org>/quicktun/releases/download/v0.1.0/quicktun-server-linux-amd64.tar.gz | tar -xz -C /opt/

# 3. 初始化数据库
sudo -u quicktun /opt/quicktun/bin/quicktun-server migrate

# 4. 创建初始 admin（提示输入密码）
sudo -u quicktun /opt/quicktun/bin/quicktun-server admin create --email=admin@yourorg.com

# 5. 安装 systemd 单元
cp /opt/quicktun/etc/systemd/quicktun-server.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now quicktun-server

# 6. TLS（自动 Let's Encrypt 或手动放）
echo "Place your TLS cert/key at /opt/quicktun/etc/tls/server.{crt,key}"
echo "Or run: certbot certonly --standalone -d relay.example.com"
```

## 2. Site agent 部署

### 2.1 跳板机要求

- Linux: 任意 amd64/arm64 distro，systemd
- Windows: Server 2019+ / Win10 1809+ (内置 OpenSSH)

### 2.2 自动安装命令

控制面 `SiteService.GetSiteInstallCommand` 返回：

**Linux**:

```bash
curl -fsSL https://relay.example.com/install/agent.sh | \
  QT_TOKEN=eyJhbGciOi... QT_ENDPOINT=relay.example.com:443 bash
```

**Windows (PowerShell)**:

```powershell
$env:QT_TOKEN="eyJhbGciOi..."
$env:QT_ENDPOINT="relay.example.com:443"
iwr -useb https://relay.example.com/install/agent.ps1 | iex
```

### 2.3 install.sh 做的事

```bash
#!/bin/bash
set -euo pipefail
: "${QT_TOKEN:?required}"
: "${QT_ENDPOINT:?required}"

# 1. 检测 OS / arch
OS=linux; ARCH=$(uname -m)
case "$ARCH" in x86_64) ARCH=amd64 ;; aarch64) ARCH=arm64 ;; esac

# 2. 创建 qt-agent 用户
useradd -r -s /usr/sbin/nologin qt-agent || true

# 3. 拉二进制（agent + 自带 rathole-client）
curl -fsSL https://relay.example.com/dl/agent/${OS}-${ARCH}/quicktun-agent.tar.gz | \
  tar -xz -C /opt/

# 4. 写初始配置 + token
mkdir -p /etc/quicktun
cat >/etc/quicktun/agent.yaml <<EOF
control_plane:
  endpoint: ${QT_ENDPOINT}
  join_token: ${QT_TOKEN}
log:
  path: /var/log/quicktun/agent.log
EOF
chmod 0600 /etc/quicktun/agent.yaml
chown qt-agent:qt-agent /etc/quicktun/agent.yaml

# 5. systemd 单元
cat >/etc/systemd/system/quicktun-agent.service <<EOF
[Unit]
Description=quicktun site agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=qt-agent
Group=qt-agent
ExecStart=/opt/quicktun-agent/bin/quicktun-agent --config /etc/quicktun/agent.yaml
Restart=always
RestartSec=2
LimitNOFILE=65536

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/etc/quicktun /var/log/quicktun
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now quicktun-agent

# 6. 状态
systemctl --no-pager status quicktun-agent
echo "✓ quicktun-agent installed."
```

### 2.4 install.ps1（Windows 关键差异）

- 用 `New-Service` 注册成 Windows Service（用 `kardianos/service` Go 库统一处理）
- 用 `New-LocalUser` 创建 `qt-agent`
- 用 `New-NetFirewallRule` 而不是 `ufw`
- 用 `icacls` 设 `agent.yaml` 权限

## 3. operator CLI 安装

### 3.1 macOS

```bash
brew install <org>/quicktun/quicktun
quicktun config init --endpoint=relay.example.com:9443
quicktun login
```

### 3.2 Linux

```bash
curl -fsSL https://github.com/<org>/quicktun/releases/latest/download/quicktun-cli-linux-amd64.tar.gz | \
  sudo tar -xz -C /usr/local/bin/
quicktun config init --endpoint=relay.example.com:9443
quicktun login
```

### 3.3 Windows

scoop bucket：

```powershell
scoop bucket add quicktun https://github.com/<org>/quicktun-scoop
scoop install quicktun
quicktun config init --endpoint=relay.example.com:9443
quicktun login
```

或下载 MSI 双击。

## 4. 备份与恢复

### 4.1 SQLite 备份

每天 cron：

```bash
#!/bin/bash
DEST=/opt/quicktun/var/backup
DATE=$(date +%Y%m%d)
sqlite3 /opt/quicktun/var/quicktun.db ".backup '$DEST/quicktun-$DATE.db'"
gzip $DEST/quicktun-$DATE.db
# 上传到对象存储 / 异地
aws s3 cp $DEST/quicktun-$DATE.db.gz s3://my-backups/
find $DEST -name 'quicktun-*.db.gz' -mtime +30 -delete
```

### 4.2 灾难恢复

1. 起新 VPS，跑 `install-server.sh`（不要 migrate）
2. 把备份的 `.db.gz` 解压回 `/opt/quicktun/var/quicktun.db`
3. 启动控制面 → 自动从 DB 拉所有 active project，重建 rathole 进程
4. 各 site 的 agent 心跳重连即可（site_agent_token 不变）

**关键**：相比 TeamViewer，"恢复整套远程访问能力"在 quicktun 里只需要 DB + TLS cert + 二进制，整个过程 < 30 分钟。

## 5. 升级

### 5.1 控制面

```bash
systemctl stop quicktun-server
curl -fsSL .../quicktun-server-vX.Y.Z-linux-amd64.tar.gz | tar -xz -C /opt/
sudo -u quicktun /opt/quicktun/bin/quicktun-server migrate  # 跑 schema migration
systemctl start quicktun-server
```

### 5.2 Agent

Phase 1 手动重跑 install.sh（脚本可识别已安装并平滑替换二进制）。Phase 2 加自更新。

## 6. 监控（推荐但 Phase 1 可选）

最小一套：

- **进程存活**：`systemctl status` + 简单 healthcheck endpoint `/healthz`
- **磁盘 / 内存**：常规 node_exporter
- **业务指标**：控制面暴露 Prometheus `/metrics`（gRPC method 调用、site online 数、token 颁发数）

## 7. 单实例 vs 高可用

Phase 1 只做单实例。HA 涉及：

- DB 改 Postgres + 主从
- 控制面无状态化（缓存外置 Redis）
- auth-proxy / rathole 需要支持多实例（端口分配状态共享）

留到 Phase 3。
