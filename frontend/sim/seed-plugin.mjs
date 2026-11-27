import path from 'node:path';
import { seedMockSource } from './seed.mjs';

/** Vite plugin: applies the 18-script simulator seed to frontend/src/api/mock.ts at transform time (the file on disk is untouched). */
export function seedMockPlugin(frontendDir, version) {
  const target = path.join(frontendDir, 'src/api/mock.ts');
  return {
    name: 'sim-seed-mock',
    enforce: 'pre',
    transform(code, id) {
      if (id.split('?')[0] !== target) return null;
      return { code: seedMockSource(code, version, target), map: null };
    },
  };
}
