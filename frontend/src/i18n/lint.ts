// Pure checkers behind the i18n tests: catalogue parity (contract 7.1) and the literal-string
// scan (7.2). Test-only: nothing in the app imports this module.
import { parse } from 'svelte/compiler';
import ts from 'typescript';
import type { CatalogData, Entry, Text } from './catalog';

const PARAM = /\{([A-Za-z_][A-Za-z0-9_]*)\}/g;
const ID = /^[a-z][A-Za-z0-9]*(\.[A-Za-z0-9_]+)+$/;

const forms = (t: Text): string[] => (typeof t === 'string' ? [t] : Object.values(t));
const empty = (t: Text | undefined): boolean => t === undefined || (typeof t === 'string' ? t === '' : Object.keys(t).length === 0);
const paramSet = (t: Text): string => [...new Set(forms(t).flatMap((s) => [...s.matchAll(PARAM)].map((m) => m[1])))].sort().join(',');
const sameText = (a: Text, b: Text): boolean => JSON.stringify(a) === JSON.stringify(b);
const longest = (t: Text): number => Math.max(...forms(t).map((s) => [...s].length));

export function validateCatalog(c: CatalogData): { problems: string[]; warnings: string[] } {
  const problems: string[] = [];
  const warnings: string[] = [];
  for (const id of Object.keys(c).sort()) {
    const e: Entry = c[id];
    const bad = (m: string) => problems.push(`${id}: ${m}`);
    if (!ID.test(id)) bad('malformed id');
    if (empty(e.en)) bad('missing en');
    if (empty(e.vi)) bad('missing vi');
    if (empty(e.en) || empty(e.vi)) continue;
    if (paramSet(e.en) !== paramSet(e.vi)) bad('param sets differ between en and vi');
    if (typeof e.en === 'object') {
      if (!e.en.one || !e.en.other) bad('en plural needs one and other');
      if (!paramSet(e.en).split(',').includes('n')) bad('plural message needs {n}');
    }
    if (typeof e.vi === 'object' && (Object.keys(e.vi).length !== 1 || !e.vi.other)) bad('vi plural must be exactly {other}');
    if (sameText(e.en, e.vi) && !e.same) bad('vi equals en (set same:true with why if intentional)');
    if (e.same && !e.why) bad('same needs why');
    for (const [lang, t] of [['en', e.en], ['vi', e.vi]] as const) {
      for (const s of forms(t)) {
        if (s === '') bad(`${lang} has an empty form`);
        if (s !== s.trim() && !s.includes('\n')) bad(`${lang} has leading/trailing space`);
        if (s.includes('  ')) bad(`${lang} has a double space`);
        if (/TODO|XXX/.test(s)) bad(`${lang} contains a TODO marker`);
      }
    }
    if (longest(e.vi) > 1.5 * longest(e.en) + 4) {
      if (e.tight) bad('tight: vi is more than 1.5x+4 the length of en');
      else warnings.push(`${id}: vi is much longer than en`);
    }
  }
  return { problems, warnings };
}

// ---- literal scan ---------------------------------------------------------------------------

export interface Hit {
  file: string;
  line: number;
  text: string;
}
export interface AllowEntry {
  file: string;
  match: string;
  reason: string;
}
export const ALLOW_REASONS = ['brand', 'key-name', 'code', 'css', 'diagnostic', 'user-content', 'overlay-status', 'dev-only', 'test-fixture'] as const;

const A11Y = new Set(['aria-label', 'aria-description', 'aria-placeholder', 'aria-roledescription', 'title', 'placeholder', 'alt']);
const NAMED = /(error|status|message|label|title|text|hint|failed|desc|description|placeholder|caption|heading|tooltip)$/i;
const SKIP_CALL = /^(console\.\w+|t|render|errorMessage|querySelector(All)?|closest|matches|addEventListener|removeEventListener|setAttribute|getAttribute|removeAttribute|matchMedia|getItem|setItem|removeItem|Symbol|require|import|String|Number|JSON\.\w+|\w+\.classList\.\w+|classList\.\w+|\w+\.(dispatchEvent|emit|on|call)|api\.(call|on)|Error|TypeError|Set|Map|Intl\.\w+)$/;

const hasLetter = (s: string): boolean => /\p{L}/u.test(s) && !ID.test(s);
/** Prose-shaped: letters plus a space, or a Capitalised word. Lone lower-case tokens are ids/css. */
const shaped = (s: string): boolean => hasLetter(s) && (/\s/.test(s.trim()) || /^\p{Lu}\p{Ll}/u.test(s.trim()));

function lineOf(src: string, pos: number): number {
  let n = 1;
  for (let i = 0; i < pos && i < src.length; i++) if (src.charCodeAt(i) === 10) n++;
  return n;
}

function scanTs(code: string, file: string, lineBase: number, hits: Hit[]): void {
  const sf = ts.createSourceFile(file, code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const lit = (n: ts.Node): string | null => {
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
    if (ts.isTemplateExpression(n)) return [n.head.text, ...n.templateSpans.map((s) => s.literal.text)].join('{}');
    return null;
  };
  const bindingName = (n: ts.Node): string => {
    let cur: ts.Node = n;
    while (cur.parent && (ts.isConditionalExpression(cur.parent) || ts.isParenthesizedExpression(cur.parent) || ts.isAsExpression(cur.parent) ||
      (ts.isBinaryExpression(cur.parent) && [ts.SyntaxKind.BarBarToken, ts.SyntaxKind.QuestionQuestionToken, ts.SyntaxKind.AmpersandAmpersandToken].includes(cur.parent.operatorToken.kind)))) {
      if (ts.isConditionalExpression(cur.parent) && cur.parent.condition === cur) return '';
      cur = cur.parent;
    }
    const p = cur.parent;
    if (!p) return '';
    if ((ts.isPropertyAssignment(p) || ts.isPropertyDeclaration(p) || ts.isVariableDeclaration(p)) && p.initializer === cur) return p.name.getText(sf);
    if (ts.isBinaryExpression(p) && p.operatorToken.kind === ts.SyntaxKind.EqualsToken && p.right === cur) return p.left.getText(sf).split('.').pop() ?? '';
    if (ts.isReturnStatement(p)) {
      for (let f: ts.Node | undefined = p; f; f = f.parent) {
        if ((ts.isFunctionDeclaration(f) || ts.isMethodDeclaration(f)) && f.name) return f.name.getText(sf);
        if (ts.isArrowFunction(f) && ts.isVariableDeclaration(f.parent)) return f.parent.name.getText(sf);
      }
    }
    return '';
  };
  const skip = (n: ts.Node): boolean => {
    for (let p = n.parent; p; p = p.parent) {
      if (ts.isImportDeclaration(p) || ts.isExportDeclaration(p) || ts.isTypeNode(p) || ts.isCaseClause(p) || ts.isThrowStatement(p)) return true;
      if (ts.isNewExpression(p) && /^(Error|TypeError|Set|Map|Intl\.\w+|RegExp)$/.test(p.expression.getText(sf))) return true;
      if (ts.isCallExpression(p) && SKIP_CALL.test(p.expression.getText(sf))) return true;
      if (ts.isElementAccessExpression(p) && p.argumentExpression === n) return true;
      if (ts.isBinaryExpression(p) && [ts.SyntaxKind.EqualsEqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsEqualsToken, ts.SyntaxKind.EqualsEqualsToken, ts.SyntaxKind.ExclamationEqualsToken].includes(p.operatorToken.kind)) return true;
      if (ts.isStatement(p)) break;
    }
    if (n.parent && ts.isPropertyAssignment(n.parent) && n.parent.name === n) return true;
    return false;
  };
  const visit = (n: ts.Node): void => {
    const s = lit(n);
    if (s !== null && hasLetter(s) && !skip(n) && (shaped(s) || NAMED.test(bindingName(n)))) {
      hits.push({ file, line: lineBase + sf.getLineAndCharacterOfPosition(n.getStart(sf)).line, text: s });
    }
    if (ts.isTemplateExpression(n)) n.templateSpans.forEach((sp) => visit(sp.expression));
    else ts.forEachChild(n, visit);
  };
  visit(sf);
}

type Node = { type: string; start?: number; end?: number; [k: string]: unknown };

function scanExpr(expr: Node, src: string, file: string, hits: Hit[], allowShapeless: boolean): void {
  const walk = (n: unknown): void => {
    if (Array.isArray(n)) return n.forEach(walk);
    if (!n || typeof n !== 'object') return;
    const o = n as Node;
    if (o.type === 'CallExpression') {
      const callee = o.callee as Node;
      if (callee.type === 'Identifier' && ['t', 'render', 'errorMessage'].includes(callee.name as string)) return;
    }
    if (o.type === 'Literal' && typeof o.value === 'string') {
      if (hasLetter(o.value) && (allowShapeless || shaped(o.value))) hits.push({ file, line: lineOf(src, o.start ?? 0), text: o.value });
      return;
    }
    if (o.type === 'TemplateElement') {
      const cooked = (o.value as { cooked?: string }).cooked ?? '';
      if (hasLetter(cooked) && (allowShapeless || shaped(cooked))) hits.push({ file, line: lineOf(src, o.start ?? 0), text: cooked });
      return;
    }
    for (const k of Object.keys(o)) if (k !== 'type' && k !== 'start' && k !== 'end') walk(o[k]);
  };
  walk(expr);
}

export function scanSource(file: string, src: string): Hit[] {
  const hits: Hit[] = [];
  if (!file.endsWith('.svelte')) {
    scanTs(src, file, 1, hits);
    return hits;
  }
  const ast = parse(src, { modern: true }) as unknown as { fragment: Node; instance?: { content: Node } };
  if (ast.instance) {
    const c = ast.instance.content;
    scanTs(src.slice(c.start, c.end), file, lineOf(src, c.start ?? 0), hits);
  }
  const walk = (n: unknown): void => {
    if (Array.isArray(n)) return n.forEach(walk);
    if (!n || typeof n !== 'object') return;
    const o = n as Node;
    if (o.type === 'Text') {
      const d = String(o.data ?? '').trim();
      if (hasLetter(d)) hits.push({ file, line: lineOf(src, o.start ?? 0), text: d.replace(/\s+/g, ' ') });
      return;
    }
    if (o.type === 'Attribute') {
      const name = String(o.name);
      const parts = Array.isArray(o.value) ? (o.value as Node[]) : o.value && o.value !== true ? [o.value as Node] : [];
      for (const p of parts) {
        if (p.type === 'Text' && A11Y.has(name) && hasLetter(String(p.data))) hits.push({ file, line: lineOf(src, p.start ?? 0), text: String(p.data).trim() });
        else if (p.type === 'ExpressionTag') scanExpr(p.expression as Node, src, file, hits, A11Y.has(name));
      }
      return;
    }
    if (o.type === 'ExpressionTag') return scanExpr(o.expression as Node, src, file, hits, false);
    for (const k of Object.keys(o)) if (k !== 'type' && k !== 'start' && k !== 'end') walk(o[k]);
  };
  walk(ast.fragment);
  return hits;
}

export interface AllowResult {
  unlisted: Hit[];
  stale: AllowEntry[];
  badReason: AllowEntry[];
}

/** Hits not covered by the allowlist, plus allowlist entries that match nothing or have no valid reason. */
export function applyAllowlist(hits: Hit[], allow: AllowEntry[], scannedFiles: string[]): AllowResult {
  const used = new Set<AllowEntry>();
  const unlisted = hits.filter((h) => {
    const a = allow.find((e) => e.file === h.file && e.match === h.text);
    if (a) used.add(a);
    return !a;
  });
  const stale = allow.filter((e) => !used.has(e) || !scannedFiles.includes(e.file));
  const badReason = allow.filter((e) => !(ALLOW_REASONS as readonly string[]).includes(e.reason));
  return { unlisted, stale, badReason };
}
