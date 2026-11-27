// Browser E2E of the simulator's editor window. Usage: PLAYWRIGHT_DIR=<dir with node_modules/playwright> node check.mjs <baseUrl> <shotsDir> [versionQuery]
// Not part of npm test (needs Playwright + a running sim or the served static build).
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
const require = createRequire(path.join(process.env.PLAYWRIGHT_DIR, 'package.json'));
const { chromium } = require('playwright');

const [base, shots, vq = ''] = process.argv.slice(2);
fs.mkdirSync(shots, { recursive: true });
const results = [];
const ok = (name, cond, detail = '') => { results.push({ name, pass: !!cond, detail }); console.log(`${cond ? 'PASS' : 'FAIL'} ${name} ${detail}`); };
const shotList = [];
const browser = await chromium.launch(process.env.PW_CHROME ? { executablePath: process.env.PW_CHROME } : {});

async function session(scaling, fn) {
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 960 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto(`${base}/?${vq}${vq ? '&' : ''}s=${scaling}`);
  const main = page.frameLocator('iframe[data-role="main"]');
  await main.getByText('Defense buff', { exact: true }).waitFor({ timeout: 30000 });
  await fn(page, main, errors);
  await ctx.close();
}
const shot = async (page, name, note) => { await page.screenshot({ path: path.join(shots, `${name}.png`) }); shotList.push({ name, note }); };
const win = (page, role) => page.locator(`.win[data-role="${role}"]`);
const box = async (loc) => loc.boundingBox();

for (const scaling of [100, 150]) {
  await session(scaling, async (page, main, errors) => {
    const tag = `${scaling}%`;
    await shot(page, `main-${scaling}`, `main window only, ${tag}`);
    ok(`${tag} one window before open`, (await page.locator('.win').count()) === 1);

    // select the first script row and open Edit
    const originalName = 'Defense buff';
    await main.getByText(originalName, { exact: true }).click();
    await main.locator('button:has-text("Edit")').click();
    await win(page, 'editor').waitFor({ state: 'attached', timeout: 10000 });
    await page.waitForSelector('.win[data-role="editor"][data-state="open"]', { timeout: 20000 });
    ok(`${tag} two windows`, (await page.locator('.win').count()) === 2);
    ok(`${tag} main blocked`, (await win(page, 'main').getAttribute('data-blocked')) === 'true');
    ok(`${tag} editor title`, /Edit Script/.test(await win(page, 'editor').locator('.txt').innerText()));
    const outer = await win(page, 'editor').getAttribute('data-outer');
    ok(`${tag} editor has its own size (not main's)`, outer !== (await win(page, 'main').getAttribute('data-outer')), outer);
    // measured size: the page must fit with no empty space / no scroll
    const ed = page.frameLocator('iframe[data-role="editor"]');
    await ed.locator('#ed-name').waitFor({ timeout: 10000 });
    await page.waitForTimeout(600);
    const metrics = await page.frames().find((f) => f.url().includes('window=editor'))?.evaluate(() => ({ m: window.__editorMetrics?.(), sw: document.documentElement.scrollWidth, cw: document.documentElement.clientWidth, sh: document.documentElement.scrollHeight, ch: document.documentElement.clientHeight }));
    ok(`${tag} editor page does not scroll its root`, metrics && metrics.sw <= metrics.cw && metrics.sh <= metrics.ch, JSON.stringify(metrics?.m ?? {}));
    // main is not clickable: a click on the blocker must not change selection
    const mb = await box(win(page, 'main'));
    const px = mb.x + mb.width / 2;
    const py = mb.y + 6; // main title bar, above the editor
    await page.mouse.click(px, py);
    const hit = await page.evaluate(([x, y]) => { const el = document.elementFromPoint(x, y); return el?.className; }, [px, py]);
    ok(`${tag} blocked main swallows the click (blocker on top, iframe pointer-events none)`, hit === 'blocker' && (await page.locator('iframe[data-role="main"]').evaluate((f) => getComputedStyle(f).pointerEvents)) === 'none', String(hit));
    await shot(page, `blocked-${scaling}`, `main blocked behind the editor, ${tag}`);
    await shot(page, `editor-${scaling}`, `main + editor, ${tag}`);

    // resize the editor from its east edge and move it
    const before = await box(win(page, 'editor'));
    const east = await box(win(page, 'editor').locator('[data-edge="e"]'));
    await page.mouse.move(east.x + east.width / 2, east.y + east.height / 2);
    await page.mouse.down();
    await page.mouse.move(east.x + east.width / 2 + 60, east.y + east.height / 2, { steps: 5 });
    await page.mouse.up();
    const after = await box(win(page, 'editor'));
    ok(`${tag} editor resizable`, after.width > before.width + 20, `${Math.round(before.width)} -> ${Math.round(after.width)}`);
    const title = await box(win(page, 'editor').locator('.title'));
    await page.mouse.move(title.x + 150, title.y + title.height / 2);
    await page.mouse.down();
    await page.mouse.move(title.x + 150 + 50, title.y + title.height / 2 + 30, { steps: 5 });
    await page.mouse.up();
    const moved = await box(win(page, 'editor'));
    ok(`${tag} editor movable`, moved.x > after.x + 10 && moved.y > after.y + 5);

    // edit the name, close with X -> guard -> save
    const newName = `Renamed ${scaling}`;
    await ed.locator('#ed-name').fill(newName);
    await win(page, 'editor').locator('.g').last().click();
    await page.waitForSelector('.win[data-role="editor"]', { state: 'detached', timeout: 15000 });
    ok(`${tag} editor closed after guard`, (await page.locator('.win').count()) === 1);
    ok(`${tag} main re-enabled`, (await win(page, 'main').getAttribute('data-blocked')) === 'false');
    await main.getByText(newName, { exact: true }).waitFor({ timeout: 8000 }).then(() => ok(`${tag} main list shows the saved name`, true, `${originalName} -> ${newName}`), () => ok(`${tag} main list shows the saved name`, false));

    // reopen: remembered bounds (the window opens where the user left it)
    await main.getByText(newName, { exact: true }).click();
    await main.locator('button:has-text("Edit")').click();
    await page.waitForSelector('.win[data-role="editor"][data-state="open"]', { timeout: 20000 });
    const again = await box(win(page, 'editor'));
    ok(`${tag} remembered bounds`, Math.abs(again.x - moved.x) < 2 && Math.abs(again.width - moved.width) < 2);
    await win(page, 'editor').locator('.g').last().click();
    await page.waitForSelector('.win[data-role="editor"]', { state: 'detached', timeout: 15000 });

    // New
    await main.locator('button:has-text("New")').first().click();
    await page.waitForSelector('.win[data-role="editor"][data-state="open"]', { timeout: 20000 });
    ok(`${tag} New opens the editor window`, /New Script/.test(await win(page, 'editor').locator('.txt').innerText()));
    await win(page, 'editor').locator('.g').last().click();
    await page.waitForSelector('.win[data-role="editor"]', { state: 'detached', timeout: 15000 });
    ok(`${tag} no page errors`, errors.length === 0, errors.join(' | '));
  });
}
await browser.close();

const fails = results.filter((r) => !r.pass);
const rows = shotList.map((s) => `<figure><img src="${s.name}.png" width="800"><figcaption>${s.name}: ${s.note}</figcaption></figure>`).join('\n');
fs.writeFileSync(path.join(shots, 'index.html'), `<!doctype html><meta charset=utf-8><title>E3 shots</title><body style="background:#111;color:#ddd;font:14px system-ui"><h1>Editor window simulator (${base})</h1><pre>${results.map((r) => `${r.pass ? 'PASS' : 'FAIL'} ${r.name} ${r.detail}`).join('\n')}</pre>${rows}`);
console.log(`\n${results.length - fails.length}/${results.length} passed`);
process.exit(fails.length ? 1 : 0);
