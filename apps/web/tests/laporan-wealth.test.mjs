import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";
import { readReviewSelection, selectionHref } from "../app/lib/cycleReview.js";

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
  assert.equal(token("chart-daily"), "#2f4a6d", "daily spending bars use single slate navy #2f4a6d");
  assert.ok(contrast(token("chart-daily"), token("surface")) >= 4.5, "chart-daily clears 4.5:1 on surface");
  const guide = text("../../docs/brand-guidelines.md");
  assert.match(guide, /Category colours[\s\S]*Retro hidup[\s\S]*contain no red and no\s+green/);
  assert.match(guide, /spent more \/ spent less than last cycle/);
  assert.match(guide, /--chart-daily.*#2f4a6d/);
});

test("spending direction colours are the muted income and expense tokens", () => {
  assert.equal(token("expense"), "#8b4437");
  assert.equal(token("income"), "#315b3f");
});

test("Laporan selection: Kekayaan is a view, /wealth defaults to it, and the URL stays clean", () => {
  assert.equal(readReviewSelection("view=wealth").view, "wealth");
  assert.equal(readReviewSelection("").view, "cycle");
  assert.equal(readReviewSelection("", "wealth").view, "wealth");
  assert.equal(readReviewSelection("view=calendar", "wealth").view, "calendar");
  assert.equal(selectionHref({ ...readReviewSelection("view=wealth&snapshotId=x&cycle=2026-08-26") }), "/analytics?view=wealth");
});
