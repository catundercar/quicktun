# quicktun deployment runbook

Walks through provisioning a fresh Linux box as a quicktun control plane and
attaching one or more bastion (agent) hosts. Phase 1 only; HA, container
images, and cert auto-issuance are out of scope.

Two host roles:

- **Control plane** — runs `quicktun-server` (gRPC + grpc-gateway) and
  `quicktun-authproxy` (CONNECT relay). Fronted by nginx for TLS.
- **Agent** — runs `quicktun-agent` (one per bastion). Spawns rathole-client.

---

## 1. Prerequisites

On the control-plane host:

- Linux x86_64, root (sudo) access.
- Two DNS records pointing at the host:
  - `CONTROL_DOMAIN`     — gRPC + HTTP gateway (e.g. `control.example.com`).
  - `RELAY_DOMAIN`       — auth-proxy front door (e.g. `relay.example.com`).
- Open inbound ports: `:443` (TLS for both gRPC and the relay).

On each agent host:

- Linux x86_64, root (sudo) access.
- Outbound `:443` reachable to `CONTROL_DOMAIN` and `RELAY_DOMAIN`.

You will also need a workstation with the `quicktun` operator CLI to drive
the control plane after install.

## 2. Install rathole

`rathole` is the actual relay backend. Download a static binary from the
upstream release page and drop it under `/usr/local/bin/`:

```bash
RATHOLE_VERSION=v0.5.0   # check https://github.com/rapiz1/rathole/releases
curl -L "https://github.com/rapiz1/rathole/releases/download/${RATHOLE_VERSION}/rathole-x86_64-unknown-linux-gnu.zip" \
    -o /tmp/rathole.zip
unzip /tmp/rathole.zip -d /tmp/rathole && \
    install -m 0755 /tmp/rathole/rathole /usr/local/bin/rathole
rathole --version
```

Do this on **both** the control plane (rathole runs as a server, supervised
by quicktun-server) and **every agent host** (rathole runs as a client,
supervised by quicktun-agent).

## 3. Install certbot

On the control plane:

```bash
apt-get install -y certbot python3-certbot-nginx
```

(or the equivalent on rpm-based distros). Don't run certbot yet; we'll do
that after nginx is configured.

## 4. Run install-server.sh

On the control-plane host:

```bash
git clone https://github.com/tulip/quicktun.git && cd quicktun
make build
sudo ./deploy/install-server.sh
```

The script:

1. Creates the `quicktun` system user and the `/etc/quicktun`,
   `/var/lib/quicktun`, `/var/log/quicktun` dirs (mode 0750, owner
   `quicktun`).
2. Copies `bin/quicktun-server` and `bin/quicktun-authproxy` to
   `/usr/local/bin/`.
3. Drops the systemd units in `/etc/systemd/system/`.
4. Copies the example configs into `/etc/quicktun/` if they're missing.
5. Runs `quicktun-server migrate` against the SQLite DB.
6. Prompts for the first admin operator's email + password (only if no
   operator exists yet — re-runs are idempotent).
7. Enables and starts both services.

If you're upgrading, re-running the script just refreshes the binaries +
units; existing configs are not touched.

## 5. Configure nginx + certbot

Install nginx with the `stream` module (default on Debian/Ubuntu).

```bash
apt-get install -y nginx
nginx -V 2>&1 | tr ' ' '\n' | grep -- --with-stream   # confirm
```

Copy the templates and replace `CONTROL_DOMAIN` / `RELAY_DOMAIN` with your
real hostnames:

```bash
cp deploy/nginx/quicktun-http.conf   /etc/nginx/sites-available/
sed -i 's/CONTROL_DOMAIN/control.example.com/g' \
    /etc/nginx/sites-available/quicktun-http.conf
ln -sf ../sites-available/quicktun-http.conf /etc/nginx/sites-enabled/

cp deploy/nginx/quicktun-stream.conf /etc/nginx/streams-available/
sed -i 's/RELAY_DOMAIN/relay.example.com/g' \
    /etc/nginx/streams-available/quicktun-stream.conf
ln -sf ../streams-available/quicktun-stream.conf /etc/nginx/streams-enabled/
```

(If your distro doesn't have `streams-enabled/`, paste the contents of
`quicktun-stream.conf` directly into the top level of `/etc/nginx/nginx.conf`
instead.)

Issue the certs:

```bash
certbot certonly --nginx \
    -d control.example.com \
    -d api.control.example.com \
    -d relay.example.com
```

Test + reload:

```bash
nginx -t && systemctl reload nginx
```

## 6. Verify the control plane

```bash
# From your workstation:
quicktun login --server https://control.example.com
quicktun project list
```

You should see an empty list and no auth errors.

你也可以通过浏览器访问 `https://control.example.com/` 进入 web 管理界面（同一端口 + 同 session token）。

## 7. Create your first project + site + service

```bash
quicktun project create my-team \
    --display-name "My Team" \
    --relay-port-range 20000-20099

quicktun site create my-team/bastion-1 \
    --display-name "Bastion 1"

quicktun service create my-team/bastion-1/ssh \
    --display-name SSH \
    --target 127.0.0.1:22 \
    --proto tcp
```

Capture the agent install token:

```bash
quicktun site get-install-command my-team/bastion-1
```

This prints a raw token. Save it for the agent install.

## 8. Per-bastion: install-agent.sh

On each bastion host:

```bash
git clone https://github.com/tulip/quicktun.git && cd quicktun
make build
sudo ./deploy/install-agent.sh \
    --token <PASTE_RAW_TOKEN> \
    --control-endpoint control.example.com:443 \
    --auth-proxy relay.example.com:443
```

The script renders `/etc/quicktun/agent.yaml` and starts
`quicktun-agent.service`. Verify:

```bash
systemctl status quicktun-agent
journalctl -u quicktun-agent -f
```

Within ~1s the agent should bootstrap, render
`/var/lib/quicktun-agent/rathole-client.toml`, and start the rathole
client.

Confirm from the operator side:

```bash
quicktun site get my-team/bastion-1   # last_seen_time should be recent
```

## 8a. macOS agent installation

The same `install-agent.sh` detects the OS automatically (`uname -s`). On
macOS it installs a **LaunchDaemon** instead of a systemd service.

**Phase 1 trade-off: the daemon runs as `root`.** A proper unprivileged
`_quicktun-agent` system user via `dscl` is deferred to Phase 2. The agent's
filesystem footprint is small and the token file is `chmod 0600`, so this is
acceptable for early adopters.

### Steps

1. Build the agent binary on macOS:

   ```bash
   git clone https://github.com/tulip/quicktun.git && cd quicktun
   make build
   ```

2. Install rathole (macOS static binary from the upstream release page):

   ```bash
   RATHOLE_VERSION=v0.5.0
   curl -L "https://github.com/rapiz1/rathole/releases/download/${RATHOLE_VERSION}/rathole-aarch64-apple-darwin.zip" \
       -o /tmp/rathole.zip
   unzip /tmp/rathole.zip -d /tmp/rathole && \
       sudo install -m 0755 /tmp/rathole/rathole /usr/local/bin/rathole
   ```

   (Use `x86_64-apple-darwin` if on Intel.)

3. Run the installer:

   ```bash
   sudo ./deploy/install-agent.sh \
       --token <PASTE_RAW_TOKEN> \
       --control-endpoint control.example.com:443 \
       --auth-proxy relay.example.com:443
   ```

   The script:
   - Creates `/etc/quicktun`, `/var/lib/quicktun-agent`, `/var/log/quicktun-agent`.
   - Copies `quicktun-agent` to `/usr/local/bin/`.
   - Drops `com.tulip.quicktun-agent.plist` into `/Library/LaunchDaemons/`.
   - Renders `/etc/quicktun/agent.yaml` (mode `0600`).
   - Calls `launchctl load -w` to enable + start the daemon immediately.

4. Verify the daemon is running:

   ```bash
   sudo launchctl list | grep quicktun-agent
   tail -f /var/log/quicktun-agent/agent.log
   ```

   A `PID` column in the `launchctl list` output confirms the process is up.

5. Stop the agent:

   ```bash
   sudo launchctl unload /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
   ```

   To prevent reloading on next boot, pass `-w` (writes the Disabled key):

   ```bash
   sudo launchctl unload -w /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
   ```

6. Upgrade: re-run `install-agent.sh` with the same flags. The script
   unloads the old daemon, replaces the binary and plist, then reloads.

---

## 8b. Windows agent installation

Phase 1 supports Windows via [NSSM](https://nssm.cc/download) wrapping the agent
as a Windows service. Tested on Windows 10/11 + Windows Server 2019/2022.

### Prerequisites
- PowerShell 5.1+ (preinstalled).
- Administrator account.
- nssm.exe (download + place beside the install script or on PATH).
- rathole.exe (download from https://github.com/rapiz1/rathole/releases) at
  `C:\Program Files\quicktun\rathole.exe`.
- A built `quicktun-agent.exe` (cross-compile from a Linux/macOS dev machine:
  `GOOS=windows GOARCH=amd64 go build -o bin/quicktun-agent.exe ./cmd/quicktun-agent`).

### Install

From PowerShell (as Administrator):

```powershell
.\deploy\windows\install-agent.ps1 `
    -Token "<RAW_TOKEN>" `
    -ControlEndpoint "control.example.com:443"
```

The script:
1. Copies `quicktun-agent.exe` to `C:\Program Files\quicktun\`.
2. Writes `C:\ProgramData\quicktun\agent.yaml` with restricted ACL (Administrators + SYSTEM only).
3. Registers a Windows service `quicktun-agent` via NSSM with auto-start + log rotation (10 MB rotation).
4. Starts the service.

### Verify

```powershell
Get-Service quicktun-agent
Get-Content -Tail 20 -Wait C:\ProgramData\quicktun\logs\agent.log
```

### Stop / remove

```powershell
.\deploy\windows\uninstall-agent.ps1            # stop + remove service only
.\deploy\windows\uninstall-agent.ps1 -Purge     # also delete config + binary
```

### Phase 1 limitations

- Service runs as `LocalSystem`. Phase 2 will add unprivileged user support
  via `nssm set quicktun-agent ObjectName <user> <password>`.
- No native `golang.org/x/sys/windows/svc` integration; we rely on NSSM as
  the wrapper. This means the agent is a console binary that NSSM
  background-runs. Acceptable for Phase 1.

---

## 9. Verify the forward

From your workstation:

```bash
quicktun forward my-team/bastion-1/ssh --local-port 2222
# In another terminal:
ssh -p 2222 user@127.0.0.1
```

You're now SSHing through the relay.

## 10. Operations

| Action                        | Command                                                      |
|-------------------------------|--------------------------------------------------------------|
| Tail server logs              | `journalctl -u quicktun-server -f`                           |
| Tail auth-proxy logs          | `journalctl -u quicktun-authproxy -f`                        |
| Tail agent logs               | `journalctl -u quicktun-agent -f`                            |
| Restart everything            | `systemctl restart quicktun-server quicktun-authproxy`       |
| Restart a single agent        | `systemctl restart quicktun-agent`                           |
| Backup the SQLite DB          | `sqlite3 /var/lib/quicktun/quicktun.db ".backup '/path/out'"` |
| Check effective config        | `quicktun-server serve --config /etc/quicktun/server.yaml --print-config` |

The SQLite file at `/var/lib/quicktun/quicktun.db` holds **all** state
(operators, projects, sites, services, sessions, agent token hashes). Back
it up. The `.backup` form above is online-safe; do NOT just `cp` an open
DB.

## 11. Troubleshooting

**Server won't start; "config: ... is required"**
The server validates config on load. Re-read `/etc/quicktun/server.yaml`
against `deploy/etc/server.yaml.example`.

**Agent says "auth_proxy_endpoint is empty"**
The control plane's `backend.auth_proxy_public_addr` is unset. Add it to
`/etc/quicktun/server.yaml` and restart `quicktun-server`.

**`quicktun login` returns 502 from nginx**
nginx's gRPC upstream is unreachable. Check `quicktun-server` is up and
listening on `127.0.0.1:9090`:
```bash
ss -ltnp | grep 9090
```

**Agent stuck "config drift"**
The control plane changed since last bootstrap. Restart the agent:
```bash
systemctl restart quicktun-agent
```

**rathole-client crashes on start**
Check `journalctl -u quicktun-agent`. Most often the rathole binary at
the configured path is wrong arch or missing execute bit.

## 12. Upgrade

Stop services, pull, rebuild, re-run install-server.sh / install-agent.sh
(both are idempotent), restart:

```bash
# Control plane:
git pull && make build
sudo ./deploy/install-server.sh
systemctl restart quicktun-server quicktun-authproxy

# Each agent:
git pull && make build
sudo ./deploy/install-agent.sh --token <SAME_TOKEN> \
    --control-endpoint control.example.com:443 \
    --auth-proxy relay.example.com:443
systemctl restart quicktun-agent
```

`migrate` runs as `ExecStartPre` on the server, so DB schema bumps apply
automatically on restart.
