---
sidebar_position: 2
---

# 安装控制面

控制面是单台 Linux VPS,跑 `quicktun-server`、`quicktun-authproxy`、`rathole`(server 模式),由 nginx 终结 TLS。

## 前置条件

- Linux x86_64,有 sudo 权限
- 两个 DNS 记录指向 VPS:
  - `CONTROL_DOMAIN`(如 `control.example.com`)— gRPC + HTTP gateway
  - `RELAY_DOMAIN`(如 `relay.example.com`)— auth-proxy 入口
- 入站 `:443` 端口开放
- 已下载并安装 [rathole](https://github.com/rapiz1/rathole/releases) 到 `/usr/local/bin/rathole`

## 步骤

### 1. 装 rathole

```bash
RATHOLE_VERSION=v0.5.0
curl -L "https://github.com/rapiz1/rathole/releases/download/${RATHOLE_VERSION}/rathole-x86_64-unknown-linux-gnu.zip" \
    -o /tmp/rathole.zip
unzip /tmp/rathole.zip -d /tmp/rathole
sudo install -m 0755 /tmp/rathole/rathole /usr/local/bin/rathole
rathole --version
```

### 2. 装 certbot + nginx

```bash
sudo apt-get install -y nginx certbot python3-certbot-nginx
nginx -V 2>&1 | tr ' ' '\n' | grep -- --with-stream  # 确认 stream 模块
```

### 3. clone + 编译

```bash
git clone https://github.com/catundercar/quicktun.git
cd quicktun
make build
```

### 4. 跑 install-server.sh

```bash
sudo ./deploy/install-server.sh
```

脚本会:

1. 创建 `quicktun` 系统用户和 `/etc/quicktun`、`/var/lib/quicktun`、`/var/log/quicktun` 目录(0750,owner `quicktun`)
2. 拷贝 `bin/quicktun-server`、`bin/quicktun-authproxy` 到 `/usr/local/bin/`
3. 安装 systemd unit 到 `/etc/systemd/system/`
4. 拷贝示例配置到 `/etc/quicktun/`(已存在则不覆盖)
5. 跑 `quicktun-server migrate`
6. 提示创建第一个 admin operator(没有则创建)
7. `systemctl enable --now` 启动服务

脚本是**幂等**的,升级时直接重跑。

### 5. 配 nginx + 申请证书

```bash
# 复制模板
sudo cp deploy/nginx/quicktun-http.conf /etc/nginx/sites-available/
sudo cp deploy/nginx/quicktun-stream.conf /etc/nginx/streams-available/

# 替换占位符
sudo sed -i 's/CONTROL_DOMAIN/control.example.com/g' /etc/nginx/sites-available/quicktun-http.conf
sudo sed -i 's/RELAY_DOMAIN/relay.example.com/g' /etc/nginx/streams-available/quicktun-stream.conf

# 启用
sudo ln -sf ../sites-available/quicktun-http.conf /etc/nginx/sites-enabled/
sudo ln -sf ../streams-available/quicktun-stream.conf /etc/nginx/streams-enabled/

# 申请证书
sudo certbot certonly --nginx \
    -d control.example.com \
    -d api.control.example.com \
    -d relay.example.com

# 测试 + reload
sudo nginx -t && sudo systemctl reload nginx
```

详细 nginx 配置见 [nginx TLS 终结](../deployment/nginx.md)。

## 验证

从你的工作站(已装 `quicktun` CLI):

```bash
quicktun login \
    --endpoint control.example.com:443 \
    --email admin@example.com \
    --password-stdin
# (输入密码)

quicktun project list
# 应该看到空列表,无报错
```

如果看到 502 / connection refused:

- 检查 `systemctl status quicktun-server quicktun-authproxy`
- 检查 nginx 是否能反代到 `127.0.0.1:9090`(gRPC)和 `127.0.0.1:8443`(authproxy):`ss -ltnp | grep -E '9090|8443'`

## 升级

```bash
cd quicktun
git pull
make build
sudo ./deploy/install-server.sh
sudo systemctl restart quicktun-server quicktun-authproxy
```

`migrate` 会作为 `ExecStartPre` 自动跑。

## 下一步

- [安装 agent](./install-agent.md) — 把跳板机加入 quicktun
- [部署 / Linux](../deployment/linux.md) — systemd unit 详解
- [配置参考 / server](../config/server.md) — `server.yaml` 字段说明
