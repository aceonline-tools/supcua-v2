// Test-support scanner: finds every button-like element in a Svelte source and reports those without an <Icon>.
const VOID = new Set(['input', 'br', 'hr', 'img', 'meta', 'link', 'area', 'base', 'col', 'embed', 'source', 'track', 'wbr']);
const ROLE_BUTTON = /(?:^|\s)role\s*=\s*(?:"\s*button\s*"|'\s*button\s*'|button(?=[\s/>]|$)|\{[^}]*["'`]\s*button\s*["'`][^}]*\})/;

/** Index just past the `}` that closes the `{` at i, honouring nesting and string literals. */
export function skipBraces(s: string, i: number): number {
  let depth = 0;
  for (; i < s.length; i++) {
    const c = s[i];
    if (c === '"' || c === "'" || c === '`') {
      for (i++; i < s.length && s[i] !== c; i++) if (s[i] === '\\') i++;
    } else if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return i + 1;
  }
  return s.length;
}

export interface ButtonEl { tag: string; line: number; inner: string }

function isButton(tag: string, attrs: string): boolean {
  return tag.toLowerCase() === 'button' || ROLE_BUTTON.test(attrs);
}

export function findButtons(src: string): ButtonEl[] {
  const out: ButtonEl[] = [];
  const stack: { tag: string; attrs: string; start: number; openEnd: number }[] = [];
  const lineOf = (i: number) => src.slice(0, i).split('\n').length;
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '{') { i = skipBraces(src, i); continue; }
    if (c !== '<') { i++; continue; }
    if (src.startsWith('<!--', i)) { const e = src.indexOf('-->', i); i = e < 0 ? src.length : e + 3; continue; }
    if (src[i + 1] === '/') {
      const m = /^<\/([A-Za-z][\w:.-]*)\s*>/.exec(src.slice(i, i + 80));
      if (!m) { i++; continue; }
      let k = stack.length - 1;
      while (k >= 0 && stack[k].tag !== m[1]) k--;
      if (k >= 0) {
        const el = stack[k];
        stack.length = k;
        if (isButton(el.tag, el.attrs)) out.push({ tag: el.tag, line: lineOf(el.start), inner: src.slice(el.openEnd, i) });
      }
      i += m[0].length;
      continue;
    }
    const nm = /^<([A-Za-z][\w:.-]*)/.exec(src.slice(i, i + 80));
    if (!nm) { i++; continue; }
    const tag = nm[1];
    const start = i;
    i += nm[0].length;
    const attrStart = i;
    let selfClose = false;
    while (i < src.length) {
      const a = src[i];
      if (a === '{') i = skipBraces(src, i);
      else if (a === '"' || a === "'") {
        for (i++; i < src.length && src[i] !== a; ) i = src[i] === '{' ? skipBraces(src, i) : i + 1;
        i++;
      } else if (a === '/' && src[i + 1] === '>') { selfClose = true; i += 2; break; }
      else if (a === '>') { i++; break; }
      else i++;
    }
    const attrs = src.slice(attrStart, i);
    if (tag === 'script' || tag === 'style') {
      const e = src.indexOf(`</${tag}>`, i);
      i = e < 0 ? src.length : e + tag.length + 3;
      continue;
    }
    if (selfClose || VOID.has(tag.toLowerCase())) {
      if (isButton(tag, attrs)) out.push({ tag, line: lineOf(start), inner: '' });
    } else stack.push({ tag, attrs, start, openEnd: i });
  }
  return out;
}

interface Frame { kind: 'root' | 'if' | 'other'; cur: boolean; branches: boolean[]; hasElse: boolean }

/** Null when the button carries an Icon outside any conditional or in every branch (else included); otherwise why not. */
export function iconProblem(inner: string): string | null {
  const root: Frame = { kind: 'root', cur: false, branches: [], hasElse: false };
  const stack: Frame[] = [root];
  const failures: string[] = [];
  let i = 0;
  while (i < inner.length) {
    const top = stack[stack.length - 1];
    if (inner[i] === '{') {
      const end = skipBraces(inner, i);
      const tok = inner.slice(i, end);
      i = end;
      if (/^\{\s*#if\b/.test(tok)) stack.push({ kind: 'if', cur: false, branches: [], hasElse: false });
      else if (/^\{\s*#/.test(tok)) stack.push({ kind: 'other', cur: false, branches: [], hasElse: false });
      else if (/^\{\s*:else/.test(tok) && top.kind === 'if') {
        top.branches.push(top.cur); top.cur = false;
        if (/^\{\s*:else\s*\}/.test(tok)) top.hasElse = true;
      } else if (/^\{\s*\//.test(tok) && stack.length > 1) {
        const f = stack.pop()!;
        const parent = stack[stack.length - 1];
        if (f.kind === 'if') {
          f.branches.push(f.cur);
          if (f.hasElse && f.branches.every(Boolean)) parent.cur = true;
          else failures.push(f.hasElse ? 'an {#if} branch has no Icon' : 'an {#if} without {:else} cannot carry the Icon');
        } else if (f.cur) parent.cur = true;
      }
      continue;
    }
    if (inner.startsWith('<!--', i)) { const e = inner.indexOf('-->', i); i = e < 0 ? inner.length : e + 3; continue; }
    if (/^<Icon[\s/>]/.test(inner.slice(i, i + 7))) top.cur = true;
    i++;
  }
  if (root.cur) return null;
  return failures[0] ?? 'no <Icon> inside';
}

export function scanSource(src: string): { line: number; problem: string }[] {
  return findButtons(src).flatMap((b) => { const p = iconProblem(b.inner); return p ? [{ line: b.line, problem: p }] : []; });
}
