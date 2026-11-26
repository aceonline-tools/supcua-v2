import { execFileSync, spawn } from 'node:child_process';
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import { seedMock } from './seed.mjs';

const simDir = path.dirname(fileURLToPath(import.meta.url));
const frontendDir = path.dirname(simDir);
const repoRoot = path.dirname(frontendDir);
const cacheDir = path.join(simDir, '.cache');
const VERSIONS = ['v1.2.1', 'v1.2.3', 'v1.3.0'];
const SHELL_PORT = Number(process.env.SIM_PORT || 5700);
const ports = Object.fromEntries(VERSIONS.map((v, i) => [v, SHELL_PORT + 1 + i]));
const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
const log = (m) => console.log(`[sim] ${m}`);
const children = [];

function git(...args) {
  return execFileSync('git', ['-C', repoRoot, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
}

function tagCommit(tag) {
  try {
    return git('rev-parse', '--verify', `${tag}^{commit}`);
  } catch {
    log(`fetching tag ${tag}`);
    git('fetch', '--no-tags', 'origin', `+refs/tags/${tag}:refs/tags/${tag}`);
    return git('rev-parse', '--verify', `${tag}^{commit}`);
  }
}

function prepare(tag) {
  const sha = tagCommit(tag);
  const dir = path.join(cacheDir, tag);
  const app = path.join(dir, 'frontend');
  const marker = path.join(dir, '.ready');
  const seedHash = crypto.createHash('sha1').update(fs.readFileSync(path.join(simDir, 'seed.mjs'))).digest('hex').slice(0, 12);
  const stamp = `${sha}:${seedHash}`;
  if (fs.existsSync(marker) && fs.readFileSync(marker, 'utf8') === stamp) {
    log(`${tag}: cached`);
    return app;
  }
  fs.rmSync(dir, { recursive: true, force: true });
  fs.mkdirSync(dir, { recursive: true });
  const tar = path.join(dir, 'src.tar');
  log(`${tag}: extracting frontend/ from ${sha.slice(0, 7)}`);
  git('archive', '-o', tar, tag, 'frontend');
  execFileSync('tar', ['-xf', tar, '-C', dir]);
  fs.rmSync(tar);
  log(`${tag}: npm ci (the tag's own lockfile)`);
  execFileSync(npm, ['ci', '--no-audit', '--no-fund'], { cwd: app, stdio: 'inherit', shell: process.platform === 'win32' });
  seedMock(path.join(app, 'src/api/mock.ts'), tag);
  fs.writeFileSync(marker, stamp);
  return app;
}

function startVite(tag, app, port) {
  return new Promise((resolve, reject) => {
    const bin = path.join(app, 'node_modules/vite/bin/vite.js');
    const child = spawn(process.execPath, [bin, '--host', '127.0.0.1', '--port', String(port), '--strictPort'], { cwd: app, stdio: ['ignore', 'pipe', 'pipe'] });
    children.push(child);
    let out = '';
    const timer = setTimeout(() => reject(new Error(`${tag}: vite did not start in 60s\n${out}`)), 60000);
    const onData = (d) => {
      out += d;
      if (/Local:/.test(out)) { clearTimeout(timer); resolve(); }
    };
    child.stdout.on('data', onData);
    child.stderr.on('data', onData);
    child.on('exit', (code) => { clearTimeout(timer); reject(new Error(`${tag}: vite exited (${code})\n${out}`)); });
  });
}

function shutdown() {
  for (const c of children) c.kill();
}
process.on('SIGINT', () => { shutdown(); process.exit(130); });
process.on('SIGTERM', () => { shutdown(); process.exit(143); });
process.on('exit', shutdown);

const t0 = Date.now();
try {
  const apps = VERSIONS.map(prepare);
  await Promise.all(VERSIONS.map((v, i) => startVite(v, apps[i], ports[v])));
  process.env.SIM_PORTS = JSON.stringify(ports);
  const shell = await createServer({ configFile: path.join(simDir, 'vite.config.ts'), server: { port: SHELL_PORT, strictPort: true, host: '127.0.0.1' } });
  await shell.listen();
  log(`ready in ${((Date.now() - t0) / 1000).toFixed(1)}s`);
  log(`open http://127.0.0.1:${SHELL_PORT}/  (Ctrl+C to stop)`);
} catch (e) {
  console.error(e.message || e);
  shutdown();
  process.exit(1);
}
