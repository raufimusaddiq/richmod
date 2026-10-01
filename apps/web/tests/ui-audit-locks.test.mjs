import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const css = () => text("app/globals.css");

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
  for (let index = 1; index <= 6; index++) {
    assert.ok(ratio(token(`chart-category-${index}`), token("surface-strong")) >= 4.5, "donut labels remain readable");
  }
});

test("type scale is tokenised and no text renders below 11px", () => {
  const source = css();
  for (const token of ["text-xs", "text-sm", "text-md", "text-base", "text-lg", "text-xl", "weight-semibold"]) assert.match(source, new RegExp(`--${token}: `));
  const sizes = [...source.matchAll(/font-size: ([0-9.]+)px/g)].map(match => Number(match[1]));
  assert.ok(sizes.length > 0);
  assert.equal(Math.min(...sizes), 11, "micro type below 11px is not allowed");
});

test("brand fonts are bundled and the guideline matches the existing token source", () => {
  const source = css();
  const guide = text("../../docs/brand-guidelines.md");
  for (const name of ["canvas", "ink", "accent", "accent-hover", "income", "expense", "warning", "danger", "info"]) {
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

test("loading, tab, and drawer states are announced to assistive technology", () => {
  assert.match(text("app/components/Feedback.js"), /role="status" aria-live="polite" aria-label="Memuat data"/);
  assert.match(text("app/inbox/page.js"), /data-view="transactions" tabIndex=/);
  assert.match(text("app/inbox/page.js"), /onKeyDown={tabKeys}/);
  assert.match(text("app/inbox/page.js"), /event\.key === "ArrowRight"/);
  assert.match(text("app/transactions/page.js"), /role="dialog" aria-modal="true" aria-label="Detail transaksi"/);
  assert.match(text("app/admin/page.js"), /function useDrawerA11y/);
  assert.match(text("app/admin/page.js"), /if \(event\.key === "Escape"\) close\(\)/);
});

test("toast dismissal is owned by a stable callback", () => {
  const feedback = text("app/components/Feedback.js");
  assert.doesNotMatch(feedback, /\}, \[message, onClose\]\)/);
  assert.match(text("app/inbox/page.js"), /<Toast message={toast} onClose={closeToast}\/>/);
  assert.match(text("app/documents/page.js"), /<Toast message={toast} onClose={closeToast}\/>/);
});

test("overview links to the exact snapshot it summarises", () => {
  assert.match(text("app/page.js"), /href={latestWealth\?\.id \? `\/wealth\?snapshotId=\$\{latestWealth\.id\}` : "\/wealth"}/);
  assert.match(text("app/wealth/page.js"), /get\("snapshotId"\)/);
  assert.match(text("app" + "/globals.css"), /\.settings-section \{ scroll-margin-top: 24px; \}/);
});

test("modal drawers share focus, Escape, and Tab handling", () => {
  const hook = text("app/components/useDrawerA11y.js");
  assert.match(hook, /event\.key === "Escape"/);
  assert.match(hook, /event\.key !== "Tab"/);
  assert.match(hook, /opener\.focus\(\)/);
  for (const page of ["transactions", "documents", "wealth"]) {
    const source = text(`app/${page}/page.js`);
    assert.match(source, /import useDrawerA11y from "\.\.\/components\/useDrawerA11y"/, `${page} imports the drawer hook`);
    assert.match(source, /<aside ref={drawerRef} tabIndex={-1}/, `${page} attaches the drawer ref`);
  }
});

test("mobile overflow button carries the pending-review badge", () => {
  const shell = text("app/components/AppShell.js");
  assert.match(shell, /hiddenInboxCount/);
  assert.match(shell, /<span>Lainnya<\/span>\{hiddenInboxCount > 0/);
});

test("user-facing copy uses the shared Indonesian vocabulary", () => {
  for (const file of ["app/admin/page.js", "app/components/ReviewCards.js", "app/settings/page.js", "app/transactions/page.js", "app/components/LandingPage.js", "app/terms/page.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /Wealth Account|Review Inbox/, `${file} avoids internal terms`);
  }
  const cards = text("app/components/ReviewCards.js");
  assert.doesNotMatch(cards, />[A-Z]{4,}( [A-Z]{2,})+</, "review card badges are sentence case");
});

test("inbox badges are decorative and the control carries the accessible name", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /<b className="nav-badge" aria-label/);
  assert.match(shell, /aria-label=\{pending \? `\$\{label\}, \$\{inboxCount\} item menunggu tinjauan`/);
  assert.match(shell, /aria-label=\{hiddenInboxCount > 0 \? `Lainnya, \$\{hiddenInboxCount\} item menunggu tinjauan`/);
});

test("admin console uses the shared Tinjauan vocabulary", () => {
  assert.doesNotMatch(text("app/admin/page.js"), /Memuat review|Per jenis review|<h2>Review<\/h2>|"Review"/);
});
