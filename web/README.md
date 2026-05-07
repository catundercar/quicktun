# quicktun 管理 SPA

Single-page admin UI for quicktun. Built with Vite + React + TypeScript + Mantine.

## Local development

```bash
npm install
npm run dev
```

The dev server proxies `/v1` and `/healthz` to `http://localhost:9091` (the
agent's gRPC-gateway HTTP endpoint). Override via env var:

```bash
QUICKTUN_API=http://192.168.1.10:9091 npm run dev
```

## Build

```bash
npm run build
```

Outputs static assets to `web/dist/`. A subsequent task will embed `dist/` into
the Go binary; until then, serve `dist/` behind any HTTP server pointing at the
agent's gRPC-gateway on the same origin.

## Layout

- `src/api/`   fetch client + manually-typed proto mirrors
- `src/auth/`  zustand auth store, login page, route guard
- `src/layout/` Mantine AppShell + sidebar nav
- `src/pages/` route components (dashboard scaffolded; resource pages stubbed)
