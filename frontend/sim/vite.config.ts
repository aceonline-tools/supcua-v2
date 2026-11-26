import path from 'node:path';
import { defineConfig } from 'vite';

export default defineConfig({
  root: path.join(import.meta.dirname, 'web'),
  cacheDir: path.join(import.meta.dirname, '.cache/.vite-shell'),
  define: { __SIM_STATIC__: 'false', __SIM_PORTS__: process.env.SIM_PORTS ?? '{}' },
  server: { host: '127.0.0.1', port: 5700, strictPort: true, allowedHosts: ['localhost', '127.0.0.1'] },
});
