import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";
import { documentTypeLabels } from "../app/lib/labels.js";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const css = () => globalCss();

const contrast = hex => {
  const channel = value => {
    const c = value / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  const [r, g, b] = [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16));
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
};

const ratio = (a, b) => {
  const [high, low] = [contrast(a), contrast(b)].sort((left, right) => right - left);
  return (high + 0.05) / (low + 0.05);
};

test("every text colour clears WCAG AA 4.5:1 on every surface", () => {
  const source = css();
  const token = name => source.match(new RegExp(`--${name}: (#[0-9a-f]{6});`))[1];
  const surfaces = ["surface", "surface-strong", "canvas", "canvas-deep", "surface-muted", "income-soft", "expense-soft", "warning-soft", "danger-soft", "info-soft", "accent-soft", "pastel-peach", "pastel-mint"];
  for (const name of ["ink", "ink-soft", "muted", "faint", "income", "expense", "warning", "danger", "info", "accent"]) {
    for (const surface of surfaces) {
      const value = ratio(token(name), token(surface));
      assert.ok(value >= 4.5, `--${name} on --${surface} is ${value.toFixed(2)}:1`);
    }
  }
  assert.ok(ratio(token("surface-strong"), token("accent-hover")) >= 4.5, "primary button hover remains readable");
  assert.ok(ratio(token("line-strong"), token("surface-strong")) >= 3, "input outlines remain visible");
  assert.ok(ratio(token("accent"), token("canvas")) >= 3, "keyboard focus remains visible");
  const hueOf = hex => {
    const [r, g, b] = [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16) / 255);
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    if (d === 0) return 0;
    const raw = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
    return (raw * 60 + 360) % 360;
  };
  const categoryHexes = new Set();
  for (let index = 1; index <= 6; index++) {
    const hex = token(`chart-category-${index}`);
    categoryHexes.add(hex);
    assert.ok(ratio(hex, token("surface")) >= 3, `chart-category-${index} clears 3:1 on surface`);
    assert.ok(ratio(hex, token("surface-strong")) >= 3, `chart-category-${index} clears 3:1 on surface-strong`);
    const h = hueOf(hex);
    assert.ok(!(h < 15 || h > 345), `chart-category-${index} is not red (${h.toFixed(0)}°)`);
    assert.ok(!(h >= 75 && h <= 160), `chart-category-${index} is not green (${h.toFixed(0)}°)`);
  }
  assert.equal(categoryHexes.size, 6, "all six category tokens are distinct");
  assert.doesNotMatch(source, /--chart-category-(?:7|8)\b/, "tokens 7 and 8 are removed");
  assert.ok(token("chart-category-other"), "Kategori lain sand token exists");
  const dailyHex = token("chart-daily");
  assert.equal(dailyHex, "#2f4a6d", "daily spending bars use single slate navy #2f4a6d");
  assert.ok(ratio(dailyHex, token("surface")) >= 4.5, `chart-daily clears 4.5:1 on surface (${ratio(dailyHex, token("surface")).toFixed(2)}:1)`);
  assert.ok(ratio(dailyHex, token("surface-strong")) >= 4.5, `chart-daily clears 4.5:1 on surface-strong`);
  const dailyHue = hueOf(dailyHex);
  assert.ok(!(dailyHue < 15 || dailyHue > 345), `chart-daily is not red (${dailyHue.toFixed(0)}°)`);
  assert.ok(!(dailyHue >= 75 && dailyHue <= 175), `chart-daily is not green or teal (${dailyHue.toFixed(0)}°)`);
  assert.notEqual(dailyHex, token("accent"), "chart-daily does not reuse accent");
  assert.ok(!categoryHexes.has(dailyHex), "chart-daily is distinct from the six category tokens");
});

test("type scale is tokenised and no text renders below 12px", () => {
  const source = css();
  for (const token of ["text-xs", "text-sm", "text-md", "text-base", "text-lg", "text-xl", "weight-semibold"]) assert.match(source, new RegExp(`--${token}: `));
  const sizes = [...source.matchAll(/font-size: ([0-9.]+)px/g)].map(match => Number(match[1]));
  assert.ok(sizes.length > 0);
  assert.equal(Math.min(...sizes), 12, "micro type below 12px is not allowed");
});

test("brand fonts are bundled and the guideline matches the existing token source", () => {
  const source = css();
  const guide = text("../../docs/brand-guidelines.md");
  for (const name of ["canvas", "ink", "accent", "accent-hover", "income", "expense", "warning", "danger", "info", "chart-daily"]) {
    const value = source.match(new RegExp(`--${name}: (#[0-9a-f]{6});`))[1];
    assert.ok(guide.includes(value), `brand guideline is stale for --${name}`);
  }
  for (const name of ["fraunces", "inter"]) {
    const file = `${name}-latin.woff2`;
    assert.match(source, new RegExp(`/fonts/${file}`));
    const font = readFileSync(new URL(`../public/fonts/${file}`, import.meta.url));
    assert.equal(font.subarray(0, 4).toString(), "wOF2");
    assert.ok(font.length < 80000, "Latin subsets remain small");
  }
  assert.match(text("public/fonts/OFL.txt"), /The Fraunces Project Authors/);
  assert.match(text("public/fonts/OFL.txt"), /The Inter Project Authors/);
  assert.doesNotMatch(source, /fonts\.googleapis\.com|fonts\.gstatic\.com/);
});

test("the stylesheet is an ordered list of balanced pieces", () => {
  const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
  const entry = read("app/globals.css");
  const imports = [...entry.matchAll(/^@import "\.\/styles\/([^"]+\.css)";$/gm)].map(match => match[1]);
  assert.ok(imports.length >= 10, "globals.css imports its pieces");
  const withoutComments = entry.replace(/\/\*[\s\S]*?\*\//g, "").split("\n").map(line => line.trim()).filter(Boolean);
  assert.ok(withoutComments.every(line => line.startsWith("@import ")), "globals.css contains only @import lines, so the cascade is the import order");
  assert.deepEqual(imports, [...imports].sort(), "pieces are imported in numeric order");
  assert.equal(new Set(imports).size, imports.length, "no piece is imported twice");
  const onDisk = readdirSync(new URL("../app/styles/", import.meta.url)).filter(name => name.endsWith(".css")).sort();
  assert.deepEqual(imports, onDisk, "every piece on disk is imported and every import exists");
  for (const name of imports) {
    const piece = read(`app/styles/${name}`).replace(/\/\*[\s\S]*?\*\//g, "").replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g, "");
    const opens = (piece.match(/\{/g) || []).length;
    const closes = (piece.match(/\}/g) || []).length;
    assert.equal(opens, closes, `${name} has balanced braces, so it ends outside any rule`);
    assert.doesNotMatch(piece, /@import/, `${name} does not import other files`);
  }
  assert.match(read("app/styles/01-tokens-and-base.css"), /:root \{/, "design tokens live in the first piece");
});

const appSources = dir => readdirSync(new URL(`../${dir}/`, import.meta.url), { withFileTypes: true })
  .flatMap(entry => entry.isDirectory() ? appSources(`${dir}/${entry.name}`) : entry.name.endsWith(".js") ? [`${dir}/${entry.name}`] : []);

test("every document type the worker can assign has an Indonesian label", () => {
  const processor = readFileSync(new URL("../../worker/internal/document/processor.go", import.meta.url), "utf8");
  const types = [...new Set(processor.match(/\b(?:RECEIPT|PAYSLIP|BANK_TRANSACTION_SCREENSHOT|TRANSFER_PROOF|EWALLET_SCREENSHOT|BILL_OR_INVOICE|TRANSACTION_HISTORY_SCREENSHOT|WEALTH_OBSERVATION|OTHER_FINANCIAL_DOCUMENT|NON_FINANCIAL_OR_UNSUPPORTED)\b/g))];
  assert.equal(types.length, 10);
  for (const type of types) assert.ok(documentTypeLabels[type], `${type} has no label`);
});

test("motion uses one scale and one reduced-motion guard", () => {
  const source = css();
  assert.equal((source.match(/prefers-reduced-motion: reduce/g) || []).length, 1, "the reduce guard is declared exactly once");
  const motion = text("app/styles/15-motion.css");
  const guard = motion.indexOf("@media (prefers-reduced-motion: reduce)");
  assert.ok(guard >= 0, "the motion piece owns the guard");
  assert.match(motion.slice(guard), /animation-duration: \.01ms !important/);
  assert.match(motion.slice(guard), /transition-duration: \.01ms !important/);
  for (const token of ["motion-fast", "motion-base", "motion-slow", "ease-standard", "ease-out"]) assert.match(source, new RegExp(`--${token}: `), `--${token} is missing from the scale`);
  assert.doesNotMatch(motion.slice(0, guard), /\b\d*\.?\d+m?s\b/, "motion rules must not hard-code a duration");
});
