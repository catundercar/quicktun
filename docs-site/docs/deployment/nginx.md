---
sidebar_position: 4
---

# nginx TLS 终结

quicktun 控制面把 TLS 全部交给 **nginx**,自己只跑 plain HTTP/TCP loopback。这一页讲怎么配 nginx。

## 为啥 nginx?

- TLS + Let's Encrypt(certbot 自动续期)生态成熟
- 同一个 :443 既要走 gRPC(`http {}` 反代)又要走 raw TCP(`stream {}` 反代),nginx 是少数几个能两个都做的反代
- 控制面 / authproxy 升级时 nginx 不重启,操作员体感无感

## 你需要的 nginx 模块

- HTTP(默认)
- `--with-http_v2_module`(默认)
- `--with-stream`(Debian/Ubuntu 默认有,RHEL 系可能要装 `nginx-mod-stream`)

```bash
nginx -V 2>&1 | tr ' ' '\n' | grep -- --with-stream
# 应该看到 --with-stream
```

## 双 listen 架构

quicktun 控制面在 `:443` 上同时跑两套协议,通过**两个 hostname** 区分:

```
control.example.com:443       → http {} → grpc_pass 127.0.0.1:9090   (gRPC)
api.control.example.com:443   → http {} → proxy_pass 127.0.0.1:9091  (REST gateway)
relay.example.com:443         → stream {} → proxy_pass 127.0.0.1:8443 (auth-proxy)
```

`control` 和 `api.control` 共用 `http {}`,SNI 区分;`relay` 用独立的 `stream {}`(因为 stream block 不能和 http block 共享 listener)。

## quicktun-http.conf

`deploy/nginx/quicktun-http.conf`,装到 `/etc/nginx/sites-available/`:

```nginx
# gRPC control plane
server {
    listen 443 ssl http2;
    server_name CONTROL_DOMAIN;

    ssl_certificate     /etc/letsencrypt/live/CONTROL_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/CONTROL_DOMAIN/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    location / {
        grpc_pass grpc://127.0.0.1:9090;
        grpc_set_header Host $host;
    }
}

# HTTP gateway (REST → gRPC)
server {
    listen 443 ssl;
    server_name api.CONTROL_DOMAIN;

    ssl_certificate     /etc/letsencrypt/live/CONTROL_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/CONTROL_DOMAIN/privkey.pem;
    ssl_protocols       TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:9091;
        proxy_set_header Host            $host;
        proxy_set_header X-Forwarded-For $remote_addr;
    }
}
```

要点:

- `listen 443 ssl http2`:gRPC 必须 HTTP/2
- `grpc_pass grpc://...`:nginx 1.13.10+ 才有
- `grpc_set_header Host $host`:把 SNI host 透传给 backend
- 两个 server 共用同一组证书(certbot 一起申请)

## quicktun-stream.conf

`deploy/nginx/quicktun-stream.conf`,根据发行版装到不同位置:

- **Debian/Ubuntu**(有 streams-available)→ `/etc/nginx/streams-available/`,然后 symlink 到 `streams-enabled/`
- 其他发行版 → 把内容直接贴到 `/etc/nginx/nginx.conf` 顶层(不要嵌套在 http 里)

```nginx
stream {
    upstream quicktun_authproxy {
        server 127.0.0.1:8443;
    }

    server {
        listen 443 ssl;
        server_name RELAY_DOMAIN;   # SNI 多路时才有意义

        ssl_certificate     /etc/letsencrypt/live/RELAY_DOMAIN/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/RELAY_DOMAIN/privkey.pem;
        ssl_protocols       TLSv1.2 TLSv1.3;

        proxy_pass quicktun_authproxy;
        proxy_protocol off;
    }
}
```

`stream {}` 块**必须**在 nginx 顶层(和 `http {}` 同级,不能嵌套)。如果你的 nginx 包没有 `streams-enabled`,就在 `nginx.conf` 末尾追加 stream 块。

## certbot

```bash
# 申请证书
sudo certbot certonly --nginx \
    -d control.example.com \
    -d api.control.example.com \
    -d relay.example.com

# certbot 默认每天检查续期(systemd timer 或 cron)
sudo systemctl status certbot.timer

# 手动测试续期
sudo certbot renew --dry-run
```

certbot **不会**自动 reload nginx,你需要 deploy hook:

```bash
sudo tee /etc/letsencrypt/renewal-hooks/deploy/nginx-reload.sh <<'EOF'
#!/bin/sh
systemctl reload nginx
EOF
sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/nginx-reload.sh
```

## 测试

```bash
# 配置语法
sudo nginx -t

# 重载(不影响在跑的连接)
sudo systemctl reload nginx

# 验证 gRPC
quicktun login --endpoint control.example.com:443 --email admin@example.com --password-stdin
# 输入密码,应该返回 "Logged in"

# 验证 REST gateway
curl https://api.control.example.com/v1/projects \
    -H "Authorization: Bearer <session_token>"

# 验证 auth-proxy(应该 401 因为没 token)
curl -v -X CONNECT https://relay.example.com:443/ 2>&1 | grep "401 Unauthorized"
```

## 故障排查

| 现象 | 排查 |
|---|---|
| 502 Bad Gateway 在 control plane | 检查 `quicktun-server` 是否在跑 + 监听 9090: `ss -ltnp \| grep 9090` |
| HTTP/1.1 from client(gRPC 报错) | 检查 nginx 用的是 `http2` 而不是 `http`;CLI 是否用了 `--insecure` 跳过 TLS |
| Stream 块没生效 | `nginx -V \| grep stream` 检查模块;确保 stream 块在 nginx 顶层 |
| `certbot renew` 失败 | nginx 需要在跑(certbot 用 webroot 验证);DNS 解析正确 |
| relay.example.com:443 直接 hang | nginx stream 配错;TLS 没终结;连不到 authproxy:8443 |

## 替代方案

不想用 nginx?可以试:

- **Caddy**:`caddy reverse_proxy` 自动管证书,但 stream 反代支持有限
- **HAProxy**:更专业的 L4 反代,但配置语法学习成本高
- **直接 TLS 在 Go 里**:authproxy 自己监听 :443 + Let's Encrypt 用 autocert。Phase 1 没做,因为 nginx 已经够用且 ops 友好

社区欢迎贡献 Caddy / HAProxy 的样例配置。
