// Playwright check of the Settings process picker in the FHD simulator.
// Usage: PLAYWRIGHT_DIR=<dir with node_modules/playwright> node process-picker.mjs <baseUrl> <shotsDir>
// Needs a running sim (SIM_TAGS=none npm run sim). Verifies the picker is a fixed box: opening it moves nothing and adds no page scroll.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
const require = createRequire(path.join(process.env.PLAYWRIGHT_DIR, 'package.json'));
const { chromium } = require('playwright');

const [base, shots] = process.argv.slice(2);
fs.mkdirSync(shots, { recursive: true });
const browser = await chromium.launch(process.env.PW_CHROME ? { executablePath: process.env.PW_CHROME } : {});
let failed = false;
const pageScrollHeight = (el) => { let n = el.parentElement; while (n && n !== document.documentElement) { if (n.scrollHeight > n.clientHeight + 1 && getComputedStyle(n).overflowY !== 'visible') return n.scrollHeight; n = n.parentElement; } return 0; };

async function run(name, query, act) {
  const page = await (await browser.newContext({ viewport: { width: 1920, height: 1080 } })).newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto(`${base}/?s=100&${query}`);
  const main = page.frameLocator('iframe[data-role="main"]');
  await main.getByText('Defense buff', { exact: true }).waitFor({ timeout: 30000 });
  await main.locator('button.nav[aria-label="Settings"]').click();
  const row = main.locator('input[aria-label="Game process name"]').locator('xpath=ancestor::div[contains(@class,"setting-row")]');
  await row.waitFor();
  const before = await row.boundingBox();
  const scroll0 = await main.locator('input[aria-label="Game process name"]').evaluate(pageScrollHeight);
  const shot = (s) => page.screenshot({ path: path.join(shots, `${name}.png`) });
  if (name === 'closed') { await shot(); }
  else {
    await main.locator('button[aria-label="Pick from running processes"]').click();
    await act(main);
    const after = await row.boundingBox();
    const m = await main.locator('.picker-pop').evaluate((el) => ({ w: el.offsetWidth, h: el.offsetHeight }));
    m.sh = await main.locator('.picker-pop').evaluate(pageScrollHeight);
    console.log(`${name.padEnd(9)} row ${before.height}->${after.height} pop=${m.w}x${m.h} pageScrollHeight ${scroll0}->${m.sh}`);
    if (before.height !== after.height || m.w !== 340 || m.h !== 240 || m.sh !== scroll0) { console.log('FAIL box/layout'); failed = true; }
    await shot();
  }
  if (errors.length) { console.log('FAIL page errors', errors); failed = true; }
  await page.close();
}

await run('closed', '', async () => {});
await run('open', '', async (m) => { await m.locator('.picker-row').first().waitFor(); });
await run('filtered', '', async (m) => { await m.locator('.picker-row').first().waitFor(); await m.locator('input[aria-label="Filter processes"]').fill('chrome'); });
await run('empty', 'procs=empty', async (m) => { await m.getByText('No windows found.').waitFor(); });
await run('error', 'procs=error', async (m) => { await m.getByText("Couldn't read the process list").waitFor(); });
await run('loading', 'procdelay=20000', async (m) => { await m.getByText('Loading…').waitFor(); });
await run('many', 'procs=many', async (m) => { await m.locator('.picker-row').first().waitFor(); });
await browser.close();
process.exit(failed ? 1 : 0);
