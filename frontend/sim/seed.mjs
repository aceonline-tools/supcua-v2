import fs from 'node:fs';

const ROWS = `
      s('a1','2.4 Missle 1','2.4 Missle','F1','infinite'),
      s('a2','2.4 Missle 2','2.4 Missle','F2','infinite'),
      s('a3','2.4 Missle 3','2.4 Missle','F3','once'),
      s('a4','2.4 Missle 4','2.4 Missle','F4','infinite'),
      s('a5','2.4 Missle 5','2.4 Missle','Ctrl + F1','once', false),
      s('b1','Defense buff','Buffs','F5','once'),
      s('b2','Shield refresh','Buffs','F6 → F7','infinite'),
      s('b3','Speed boost','Buffs','Alt + 1','once'),
      s('b4','HP potion','Buffs','Alt + 2','once'),
      s('b5','MP potion','Buffs','Alt + 3','once', false),
      s('c1','Auto attack','Farming','Q','infinite'),
      s('c2','Collect loot','Farming','Ctrl + Shift + 1','once'),
      s('c3','Repair cycle','Farming','F9','once', false),
      s('c4','Return to town','Farming','Ctrl + R','once'),
      s('d1','Summon pet','Utility','Shift + F1','once'),
      s('d2','Open inventory','Utility','I','once'),
      s('d3','Swap weapon set','Utility','Tab','once'),
      s('d4','Anti-idle','Utility','Ctrl + Alt + A','infinite'),`;

/** Rewrites the cached copy's mock seed only: 18 scripts in 4 groups, system theme, the tag as version. */
export function seedMock(file, version) {
  let t = fs.readFileSync(file, 'utf8');
  const steps = [
    [/scripts: \[\n[\s\S]*?\n    \],\n    settings/, `scripts: [${ROWS}\n    ],\n    settings`],
    ["theme: 'dark', global_hotkey", "theme: 'system', global_hotkey"],
    ["      theme: 'dark', mouse_locked", "      theme: 'system', mouse_locked"],
    ["global_active: true, running_id: '', running_name: ''", "global_active: true, running_id: 'c1', running_name: 'Auto attack'"],
    ["version: 'v1.0.28'", `version: '${version}'`],
  ];
  for (const [from, to] of steps) {
    const next = t.replace(from, to);
    if (next === t) throw new Error(`mock seed: pattern not found in ${file}: ${String(from).slice(0, 40)}`);
    t = next;
  }
  fs.writeFileSync(file, t);
}
