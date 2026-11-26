import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { seedMockSource } from './seed.mjs';

const simDir = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.dirname(simDir);
const out = path.join(simDir, 'dist-static');
const version = 'main';

const seedMock = {
  name: 'sim-seed-mock',
  enforce: 'pre',
  transform(code, id) {
    const file = id.split('?')[0];
    if (file !== path.join(frontendDir, 'src/api/mock.ts')) return null;
    return { code: seedMockSource(code, version, file), map: null };
  },
};

await build({
  configFile: false,
  root: path.join(simDir, 'web'),
  base: './',
  logLevel: 'warn',
  define: { __SIM_STATIC__: 'true', __SIM_PORTS__: '{}' },
  build: { outDir: out, emptyOutDir: true, target: 'es2022', sourcemap: false },
});

await build({
  configFile: false,
  root: frontendDir,
  base: './',
  logLevel: 'warn',
  plugins: [seedMock, svelte()],
  define: { 'import.meta.env.DEV': 'true' },
  build: { outDir: path.join(out, 'app'), emptyOutDir: true, target: 'es2022', assetsInlineLimit: 0, sourcemap: false },
});
