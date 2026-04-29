# quicktun

Multi-site remote access control plane. Replaces TeamViewer/ToDesk-style screen sharing for managing many small project networks; each network gets a bastion machine running the quicktun agent and is reachable via SSH / RDP / AI tools through a central relay.

> Phase 1 is bastion + reverse-proxy (rathole). Phase 2 adds NetBird mesh. See `docs/`.

## Status

Active development. Phase 1 foundation complete (M1: data model + migration runner).

## Quick start (developers)

```bash
git clone <repo>
cd quicktun
make build
mkdir -p var
cp etc/server.example.yaml etc/server.yaml
# edit etc/server.yaml — at minimum set database.dsn
./bin/quicktun-server migrate --config etc/server.yaml
./bin/quicktun-server version
```

## Design docs

| Doc | Topic |
|-----|-------|
| [docs/00-overview.md](docs/00-overview.md) | Product framing + key decisions |
| [docs/01-data-model.md](docs/01-data-model.md) | GORM models + ER + migrations |
| [docs/02-grpc-api.md](docs/02-grpc-api.md) | gRPC + grpc-gateway (Google AIP) |
| [docs/03-agent-protocol.md](docs/03-agent-protocol.md) | Site agent ↔ control plane |
| [docs/04-security.md](docs/04-security.md) | Network admission + auth-proxy |
| [docs/05-process-supervisor.md](docs/05-process-supervisor.md) | rathole / auth-proxy lifecycle |
| [docs/06-cli.md](docs/06-cli.md) | Operator CLI |
| [docs/07-deployment.md](docs/07-deployment.md) | Server + agent deployment |
| [docs/08-roadmap.md](docs/08-roadmap.md) | Milestones |

## License

TBD.
