import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022', assetsInlineLimit: 0, sourcemap: false },
  resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
  test: { environment: 'jsdom', include: ['src/**/*.test.ts'], css: { include: [/app\.css/] } },
});
