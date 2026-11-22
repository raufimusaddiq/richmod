import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";
import { tree, globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const token = name => globalCss().match(new RegExp(`--${name}: (#[0-9a-f]{6});`))[1];
const channel = (hex, index) => parseInt(hex.slice(1 + index * 2, 3 + index * 2), 16) / 255;
const luminance = hex => {
  const [r, g, b] = [0, 1, 2].map(index => channel(hex, index)).map(value => value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrast = (a, b) => { const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p); return (x + 0.05) / (y + 0.05); };
function hue(hex) {
  const [r, g, b] = [0, 1, 2].map(index => channel(hex, index));
  const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
  if (d === 0) return 0;
  const raw = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return (raw * 60 + 360) % 360;
}

test("category colours use the six Retro hidup tokens with no red or green and clear 3:1 on surface", () => {
  const accent = token("accent");
  const expected = ["#1f6f68", "#b07d12", "#4a78a8", "#8a4f86", "#b8602a", "#7d68b3"];
  const seen = new Set();
  for (let index = 1; index <= 6; index++) {
    const value = token(`chart-category-${index}`);
    assert.equal(value, expected[index - 1], `chart-category-${index} matches Retro hidup`);
    assert.ok(!seen.has(value), `chart-category-${index} must be distinct`);
    seen.add(value);
    assert.notEqual(value, accent, `chart-category-${index} must not reuse --accent`);
    assert.ok(contrast(value, token("surface")) >= 3, `chart-category-${index} contrast on surface`);
    const h = hue(value);
    assert.ok(!(h < 15 || h > 345), `chart-category-${index} is not red (${h.toFixed(0)}°)`);
    assert.ok(!(h >= 75 && h <= 160), `chart-category-${index} is not green (${h.toFixed(0)}°)`);
  }
  assert.equal(seen.size, 6, "six distinct category colours exist");
  assert.doesNotMatch(globalCss(), /--chart-category-(?:7|8)\b/, "tokens 7 and 8 are removed");
  assert.equal(token("chart-category-other"), "#cfc9b6", "Kategori lain keeps neutral sand");
  const guide = text("../../docs/brand-guidelines.md");
  assert.match(guide, /Category colours[\s\S]*Retro hidup[\s\S]*contain no red and no\s+green/);
  assert.match(guide, /spent more \/ spent less than last cycle/);
});

test("spending direction colours are the muted income and expense tokens", () => {
  const styles = globalCss();
  assert.match(styles, /\.kpi-delta\[data-tone="good"\] strong \{ color: var\(--income\); \}/);
  assert.match(styles, /\.kpi-delta\[data-tone="bad"\] strong \{ color: var\(--expense\); \}/);
  assert.match(styles, /\.diverge-value \.delta\[data-tone="bad"\] \{ color: var\(--expense\); \}/);
  assert.equal(token("expense"), "#8b4437");
  assert.equal(token("income"), "#315b3f");
});

test("direction wording is neutral everywhere in the report", () => {
  const source = tree("app/analytics") + text("app/lib/cycleKpi.js");
  assert.match(source, /◀ lebih rendah/);
  assert.match(source, /lebih tinggi ▶/);
  assert.doesNotMatch(source, /hemat|boros/);
});

test("Laporan selection: Kekayaan is a view, /wealth defaults to it, and the URL stays clean", () => {
  assert.equal(readReviewSelection("view=wealth").view, "wealth");
  assert.equal(readReviewSelection("").view, "cycle");
  assert.equal(readReviewSelection("", "wealth").view, "wealth");
  assert.equal(readReviewSelection("view=calendar", "wealth").view, "calendar");
  assert.equal(selectionHref({ ...readReviewSelection("view=wealth&snapshotId=x&cycle=2026-08-26") }), "/analytics?view=wealth");
});

test("Laporan renders an accessible Arus kas | Kekayaan tablist above both workspaces", () => {
  const page = text("app/analytics/page.js");
  assert.match(page, /role="tablist" aria-label="Jenis laporan"/);
  assert.match(page, /role="tab"/);
  for (const key of ["ArrowRight", "ArrowLeft", "Home", "End"]) assert.ok(page.includes(key), key);
  assert.match(page, /aria-selected=\{active === id\}/);
  assert.match(page, /tabIndex=\{active === id \? 0 : -1\}/);
  assert.match(page, /<WealthWorkspace user=\{user\}\/>/);
  assert.match(page, /title="Laporan"/);
});

test("/wealth keeps working by rendering the same Laporan page in wealth mode", () => {
  assert.equal(text("app/wealth/page.js").trim(), 'export { default } from "../analytics/page";');
  assert.match(text("app/analytics/page.js"), /=== "\/wealth" \? "wealth" : "cycle"/);
  const workspace = text("app/wealth/WealthWorkspace.js");
  assert.match(workspace, /get\("snapshotId"\)/);
  assert.match(workspace, /get\("observationId"\)/);
  assert.match(workspace, /dropQueryParam\("snapshotId"\)/);
  assert.match(workspace, /dropQueryParam\("observationId"\)/);
  assert.match(workspace, /href="\/household\?tab=akun#wealth-accounts"/);
  assert.doesNotMatch(workspace, /AppShell/);
});

test("each cycle card shows its Go takeaway above one Detail disclosure, with no duplicate sections", () => {
  const source = tree("app/analytics");
  for (const key of ["where", "history", "changes", "pace"]) assert.ok(source.includes(`facts.takeaways?.${key}`), key);
  assert.doesNotMatch(source, /<section id="matrix"|<section id="merchants"|SavingsWealthCard/);
  assert.match(source, /Angka per kategori ›/);
  assert.match(source, /Per kategori &amp; merchant ›/);
  const kpi = text("app/lib/cycleKpi.js");
  assert.doesNotMatch(kpi, /key: "worth"|key: "unallocated"/);
});

test("supporting cards are calm; the KPI strip keeps the ink border and shadow without a refund footer", () => {
  const styles = globalCss();
  assert.match(styles, /\.dash-card, \.cycle-review \.review-stack \{ border: 1\.5px solid var\(--line-strong\); background: var\(--surface\); box-shadow: none; \}/);
  assert.match(styles, /\.kpi-strip dl \{ border: 2px solid var\(--ink\); box-shadow: var\(--shadow\); \}/);
  const dashboard = text("app/analytics/CycleDashboard.js");
  assert.doesNotMatch(dashboard, /kpi-note|Tentang posisi siklus/);
});

test("Ke mana uangnya shows amount and share only, with no delta column", () => {
  const where = text("app/analytics/CycleWhere.js");
  const rankRow = where.slice(where.indexOf("function RankRow"), where.indexOf("export function ChangesCard"));
  assert.match(rankRow, /className="where-swatch"/);
  assert.match(rankRow, /className="where-amount"/);
  assert.match(rankRow, /className="where-share">\{ratioLabel\(row\.shareOfExpense\)\}/);
  assert.doesNotMatch(rankRow, /where-delta|<Delta/);
});
