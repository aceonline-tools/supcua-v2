// Playwright check of Settings > "Check for updates" in the FHD simulator.
// Usage: PLAYWRIGHT_DIR=<dir with node_modules/playwright> node update-check.mjs <baseUrl> <shotsDir>
// Needs a running sim (SIM_TAGS=none npm run sim). Measures the License row height in every state.
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
const require = createRequire(path.join(process.env.PLAYWRIGHT_DIR, 'package.json'));
const { chromium } = require('playwright');

const [base, shots] = process.argv.slice(2);
fs.mkdirSync(shots, { recursive: true });
const browser = await chromium.launch(process.env.PW_CHROME ? { executablePath: process.env.PW_CHROME } : {});
const rows = [];
let failed = false;

async function run(name, query, finalText, errorCase) {
  const ctx = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  await page.goto(`${base}/?s=100&${query}`);
  const main = page.frameLocator('iframe[data-role="main"]');
  await main.getByText('Defense buff', { exact: true }).waitFor({ timeout: 30000 });
  await main.locator('button.nav[aria-label="Settings"]').click();
  const row = main.locator('.upd-ctl').locator('xpath=ancestor::div[contains(@class,"setting-row")]');
  await row.waitFor();
  const measure = async (state) => {
    const r = await row.boundingBox();
    const b = await main.locator('.upd-btn').boundingBox();
    const t = await main.locator('.upd-status').boundingBox();
    const text = await main.locator('.upd-status').innerText();
    rows.push({ scenario: name, state, rowH: r.height, btnW: b.width, btnH: b.height, statusW: t.width, text });
    console.log(`${name.padEnd(13)} ${state.padEnd(10)} row=${r.height}px btn=${b.width}x${b.height} status=${t.width}px "${text}"`);
  };
  const shot = (state) => page.screenshot({ path: path.join(shots, `${name}-${state}.png`) });
  if (name === 'up_to_date') { await measure('idle'); await shot('idle'); }
  await main.locator('button[aria-label="Check for updates"]').click();
  if (name === 'up_to_date') { await main.getByText('Checking…').first().waitFor(); await measure('checking'); await shot('checking'); }
  await main.locator('.upd-status', { hasText: finalText }).waitFor({ timeout: 10000 });
  await measure('result');
  await shot('result');
  if (errorCase && !(await main.locator('.upd-status.upd-err').count())) { console.log('FAIL error style missing'); failed = true; }
  if (errors.length) { console.log('FAIL page errors', errors); failed = true; }
  await ctx.close();
}

await run('up_to_date', 'upddelay=1500', 'Up to date (');
await run('available', 'updcheck=available', 'Update available v1.0.29');
await run('offline', 'updcheck=offline', "Can't reach GitHub", true);
await run('rate_limited', 'updcheck=rate_limited', 'rate limit', true);
await run('other', 'updcheck=other', 'Update check failed', true);
await browser.close();
const hs = new Set(rows.map((r) => r.rowH.toFixed(1)));
const ws = new Set(rows.map((r) => `${r.btnW.toFixed(1)}x${r.btnH.toFixed(1)}|${r.statusW.toFixed(1)}`));
console.log(`row heights seen: ${[...hs].join(', ')}; button/status boxes seen: ${[...ws].join(', ')}`);
if (hs.size !== 1 || ws.size !== 1) { console.log('FAIL row or controls changed size between states'); failed = true; }
fs.writeFileSync(path.join(shots, 'measurements.json'), JSON.stringify(rows, null, 2));
process.exit(failed ? 1 : 0);
