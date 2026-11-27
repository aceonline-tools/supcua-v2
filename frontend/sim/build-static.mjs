import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { seedMockPlugin } from './seed-plugin.mjs';

const simDir = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.dirname(simDir);
const out = path.join(simDir, 'dist-static');
const version = 'main';

await build({
  configFile: false,
  root: path.join(simDir, 'web'),
  base: './',
  plugins: [seedMockPlugin(frontendDir, version)],
  logLevel: 'warn',
  define: { __SIM_STATIC__: 'true', __SIM_PORTS__: '{}' },
  build: { outDir: out, emptyOutDir: true, target: 'es2022', sourcemap: false },
});

await build({
  configFile: false,
  root: frontendDir,
  base: './',
  logLevel: 'warn',
  plugins: [seedMockPlugin(frontendDir, version), svelte()],
  define: { 'import.meta.env.DEV': 'true' },
  build: { outDir: path.join(out, 'app'), emptyOutDir: true, target: 'es2022', assetsInlineLimit: 0, sourcemap: false },
});
