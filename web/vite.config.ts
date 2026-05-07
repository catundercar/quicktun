import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// QUICKTUN_API points at the agent-side gRPC-gateway HTTP endpoint.
// Default matches the smoke server defaults; override for non-local backends.
const apiTarget = process.env.QUICKTUN_API ?? 'http://localhost:9091';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/v1': { target: apiTarget, changeOrigin: true },
      '/healthz': { target: apiTarget, changeOrigin: true },
    },
  },
});
