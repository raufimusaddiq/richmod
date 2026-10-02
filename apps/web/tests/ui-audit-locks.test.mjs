import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import { reviewCards, tree, globalCss } from "./source.mjs";

const text = path => path.endsWith("/") ? tree(path.slice(0, -1)) : readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
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
  for (let index = 1; index <= 6; index++) {
    assert.ok(ratio(token(`chart-category-${index}`), token("surface-strong")) >= 4.5, "donut labels remain readable");
  }
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
  assert.match(text("app/components/Feedback.js"), /role="status" aria-live="polite" aria-label=\{label\}/);
  assert.match(text("app/components/Feedback.js"), /label = "Memuat data"/);
  assert.doesNotMatch(text("app/page.js"), /role="status" aria-live="polite">\{loading && <Skeleton/);
  assert.match(text("app/inbox/page.js"), /data-view="transactions" tabIndex=/);
  assert.match(text("app/inbox/page.js"), /onKeyDown={tabKeys}/);
  assert.match(text("app/inbox/page.js"), /event\.key === "ArrowRight"/);
  assert.match(text("app/transactions/page.js"), /role="dialog" aria-modal="true" aria-label="Detail transaksi"/);
  assert.match(tree("app/admin"), /function useDrawerA11y/);
  assert.match(tree("app/admin"), /if \(event\.key === "Escape"\) close\(\)/);
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
  assert.match(globalCss(), /\.settings-section \{ scroll-margin-top: 24px; \}/);
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
  for (const file of ["app/admin/", "app/components/review/", "app/settings/page.js", "app/transactions/page.js", "app/components/LandingPage.js", "app/terms/page.js", "app/privacy/page.js", "app/inbox/page.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /Wealth Account|Review Inbox|Pemilik household|data household|alamat household|Antrean review|Joint \/ household/, `${file} avoids internal terms`);
  }
  const cards = reviewCards();
  assert.doesNotMatch(cards, />[A-Z]{4,}( [A-Z]{2,})+</, "review card badges are sentence case");
});

test("inbox badges are decorative and the control carries the accessible name", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /<b className="nav-badge" aria-label/);
  assert.match(shell, /aria-label=\{pending \? `\$\{label\}, \$\{inboxCount\} item menunggu tinjauan`/);
  assert.match(shell, /aria-label=\{hiddenInboxCount > 0 \? `Lainnya, \$\{hiddenInboxCount\} item menunggu tinjauan`/);
});

test("admin console uses the shared Tinjauan vocabulary", () => {
  assert.doesNotMatch(tree("app/admin"), /Memuat review|Per jenis review|<h2>Review<\/h2>|"Review"/);
});

test("destructive and text-entry choices use the shared dialog, not window.confirm/prompt", () => {
  for (const file of ["app/settings/page.js", "app/household/page.js", "app/admin/", "app/components/CycleDecisions.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /window\.(confirm|prompt)\(|if \(!confirm\(/, `${file} avoids native dialogs`);
    assert.match(source, /useDialogs/, `${file} uses the shared dialog hook`);
  }
  const dialogs = text("app/components/useDialogs.js");
  assert.match(dialogs, /showModal\(\)/);
  assert.match(dialogs, /onCancel=/);
  assert.match(dialogs, /useId\(\)/);
  assert.doesNotMatch(dialogs, /app-dialog-title/);
  // The leave-page guard in analytics must answer synchronously, so it keeps window.confirm.
  assert.match(text("app/analytics/page.js"), /window\.confirm\("Ada draf keputusan/);
});

test("the shell shares one inbox count instead of fetching both lists per navigation", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /fetch\("\/api\/v1\/reviews"\)/);
  assert.match(shell, /useInboxCount\(\)/);
  assert.match(shell, /^function NavLink\(/m, "NavLink is a top-level component");
  assert.doesNotMatch(shell, /^[ \t]+function NavLink\(/m, "NavLink is not redefined inside AppShell");
  const provider = text("app/components/InboxCountProvider.js");
  assert.match(provider, /Date\.now\(\) - lastLoad\.current > 60000/);
  assert.match(provider, /INBOX_COUNT_EVENT/);
  assert.match(text("app/layout.js"), /<InboxCountProvider>\{children\}<\/InboxCountProvider>/);
  assert.match(text("app/inbox/page.js"), /if \(!loading && !error\) publishInboxCount\(reviews\.length \+ actions\.length\)/);
});

test("decorative glyphs are hidden from assistive technology", () => {
  assert.match(text("app/components/Feedback.js"), /<span aria-hidden="true">✓<\/span>/);
  assert.match(reviewCards(), /<span aria-hidden="true">✓<\/span>/);
  assert.match(text("app/inbox/page.js"), /<span aria-hidden="true">✓<\/span>/);
  assert.doesNotMatch(text("app/inbox/page.js"), /<span>✓<\/span>/);
});

test("navigation icons are keyed by name, not by glyph", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /\["[^"]*", "[^"]*", "[⌂⌁✓▤⌾]"\]/);
  assert.doesNotMatch(shell, /"[⌂⌁✓▤⌾]": /);
  for (const name of ["home", "analytics", "inbox", "documents", "household"]) {
    assert.match(shell, new RegExp(`${name}: \\w+`), `${name} maps to an icon`);
  }
});

test("a failed request names what is missing and keeps the rest", () => {
  const home = text("app/page.js");
  assert.match(home, /const sections = \[/);
  assert.match(home, /sections\.map\(\(\[, url\]\) => fetch\(url\)\)/);
  assert.doesNotMatch(home, /sectionNames/);
  assert.match(home, /Belum termuat: \$\{failed\.join/);
  assert.doesNotMatch(home, /new Date\(latestWealth\?\.observedAt\)/);
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /Each list stands on its own/);
  assert.doesNotMatch(inbox, /if \(!reviewResponse\.ok \|\| !actionResponse\.ok\) throw new Error/);
});

test("large screens are split into modules and carry no inline checkbox styles", () => {
  const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
  const lines = path => read(path).split("\n").length;
  assert.ok(lines("app/admin/page.js") < 120, "admin page.js is only the tab shell");
  for (const tab of ["Overview", "Reviews", "Jobs", "LLM", "Logs", "Households", "Users", "Audit"]) {
    assert.match(read(`app/admin/${tab}.js`), new RegExp(`export default function ${tab}\\(`), `${tab} lives in its own module`);
  }
  assert.match(read("app/admin/page.js"), /import Overview from "\.\/Overview"/);
  assert.ok(lines("app/components/ReviewCards.js") < 30, "ReviewCards.js only routes items to their card");
  for (const card of ["CanonicalCard", "ReviewCard", "TransferCard", "ResidualCard", "WealthObservationCard", "FinancialEmailResolutionCard", "TransferReconciliationCard"]) {
    assert.match(read(`app/components/review/${card}.js`), new RegExp(`export default function ${card}\\(`), `${card} lives in its own module`);
  }
  assert.doesNotMatch(reviewCards(), /checkbox(Label|Input)Style|style=\{\{/, "review cards use CSS classes, not inline styles");
  assert.match(css(), /label\.review-check \{ display: flex;/);
  assert.match(css(), /label\.review-check input\[type="checkbox"\] \{ width: 16px;/);
});

test("the households table uses the shared Indonesian vocabulary", () => {
  const households = readFileSync(new URL("../app/admin/Households.js", import.meta.url), "utf8");
  for (const header of ["Keluarga", "Anggota", "Transaksi", "Aktivitas terakhir", "Dibuat"]) assert.match(households, new RegExp(`"${header}"`));
  assert.doesNotMatch(households, /"Last activity"|"Members"|"Created"/);
});

test("split modules import only what they use", () => {
  const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
  for (const card of ["CanonicalCard", "FinancialEmailResolutionCard", "ResidualCard", "ReviewCard", "TransferCard", "WealthObservationCard"]) {
    assert.doesNotMatch(read(`app/components/review/${card}.js`), /import \{[^}]*\blabel\b[^}]*\} from "\.\/shared"/, `${card} does not import an unused label`);
  }
});

test("every dashboard section has a setter and every nav entry has an icon", () => {
  const home = text("app/page.js");
  const names = [...home.matchAll(/^\s+\["([^"]+)", "\/api\/v1\/[^"]+"\],$/gm)].map(match => match[1]);
  assert.ok(names.length >= 6, "the dashboard sections are listed with their endpoints");
  for (const name of names) assert.match(home, new RegExp(`"${name}": `), `section "${name}" has a setter in apply`);
  assert.match(home, /apply\[name\]\(await responses\[index\]\.json\(\)\)/);

  const shell = text("app/components/AppShell.js");
  const navKeys = [...shell.matchAll(/^\s+\["\/[^"]*", "[^"]+", "(\w+)"\],$/gm)].map(match => match[1]);
  const iconMap = shell.match(/const icons = \{([^}]*)\}/)[1];
  assert.ok(navKeys.length >= 8);
  for (const key of [...navKeys, "admin"]) assert.match(iconMap, new RegExp(`\\b${key}: `), `nav icon "${key}" is mapped`);
});

test("one malformed inbox list does not hide the other", () => {
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /const readList = async \(response, apply, name\) => \{ try \{/);
  assert.match(inbox, /await readList\(reviewResponse, setReviews/);
  assert.match(inbox, /await readList\(actionResponse, setActions/);
});

test("the console docs point at the split admin modules", () => {
  for (const doc of ["RICHMOD_SUPER_ADMIN_CONSOLE_FINALIZATION_CODEX.md", "RICHMOD_SUPER_ADMIN_PLATFORM_CONSOLE_CODEX.md"]) {
    const source = readFileSync(new URL(`../../../docs/${doc}`, import.meta.url), "utf8");
    assert.doesNotMatch(source, /^apps\/web\/app\/admin\/page\.js$/m, `${doc} no longer names the old single file`);
    assert.doesNotMatch(source, /app\/admin\/components/, `${doc} no longer suggests an admin/components layout`);
  }
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

test("an unmapped nav icon falls back instead of rendering undefined", () => {
  assert.match(text("app/components/AppShell.js"), /const Icon = icons\[icon\] \|\| DotsThree;/);
});

test("metric strips in cycle review stay rounded tiles, not flat square blocks", () => {
  const rule = css().match(/\.cycle-review \.review-context \{[^}]*\}/)?.[0] ?? "";
  assert.match(rule, /border-radius: var\(--radius-md\)/);
  assert.match(rule, /border: 1px solid var\(--line\)/);
  assert.match(rule, /background: var\(--surface\)/);
  assert.doesNotMatch(rule, /border-radius: 0/);
});

test("the category donut does not animate in, so it never captures or shows a half-drawn ring", () => {
  const donut = text("app/components/Charts.js").match(/export function CategoryDonutChart[\s\S]*?\n\}/)?.[0] ?? "";
  assert.match(donut, /<Pie [^>]*isAnimationActive=\{false\}/);
});

test("legal pages put the brand on its own line above the page label", () => {
  assert.match(css(), /\.public-legal-brand \{ display: block;/);
});
