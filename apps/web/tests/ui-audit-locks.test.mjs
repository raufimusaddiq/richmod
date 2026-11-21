import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";
import { reviewCards, tree, globalCss } from "./source.mjs";
import { documentTypeLabels } from "../app/lib/labels.js";
import { money } from "../app/lib/format.js";

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

test("the mobile bar is four tabs around one elevated add button, with no overflow menu", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /hiddenInboxCount|Lainnya|moreOpen/);
  const bar = shell.slice(shell.indexOf('<nav className="mobile-nav"'));
  const order = [...bar.matchAll(/item=\{(\w+)\}|className="mobile-add"/g)].map(match => match[1] || "add");
  assert.deepEqual(order, ["homeLink", "ledgerLink", "add", "reportLink", "familyLink"], "Beranda · Transaksi · + · Laporan · Keluarga");
  const css = text("app/styles/11-responsive.css");
  assert.match(css, /\.mobile-nav \.mobile-add \{[^}]*min-width: 52px; min-height: 52px;[^}]*border: 2px solid var\(--ink\)/);
  assert.match(css, /\.mobile-nav \{[^}]*calc\(7px \+ env\(safe-area-inset-bottom\)\)/);
  assert.match(css, /\.review-pill \{ display: inline-flex; min-height: 44px;/);
});

test("user-facing copy uses the shared Indonesian vocabulary", () => {
  for (const file of ["app/admin/", "app/components/review/", "app/components/SettingsPanels.js", "app/transactions/page.js", "app/components/LandingPage.js", "app/terms/page.js", "app/privacy/page.js", "app/inbox/page.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /Wealth Account|Review Inbox|Pemilik household|data household|alamat household|Antrean review|Joint \/ household/, `${file} avoids internal terms`);
  }
  const cards = reviewCards();
  assert.doesNotMatch(cards, />[A-Z]{4,}( [A-Z]{2,})+</, "review card badges are sentence case");
});

test("inbox badges are decorative and the control carries the accessible name", () => {
  const shell = text("app/components/AppShell.js");
  assert.doesNotMatch(shell, /<b className="nav-badge" aria-label/);
  assert.match(shell, /aria-label=\{pending \? .\$\{label\}, \$\{inboxCount\} item menunggu keputusan./);
});

test("Perlu dicek is conditional: the link and the phone pill render only with decisions waiting", () => {
  const shell = text("app/components/AppShell.js");
  assert.match(shell, /\{reviewCount > 0 && <NavLink item=\{inboxItem\}/);
  assert.match(shell, /\{reviewCount > 0 && <Link className="review-pill" href="\/inbox">\{reviewCount\} perlu dicek ›<\/Link>\}/);
  assert.doesNotMatch(shell, /^  \["\/inbox", "Perlu dicek", "inbox"\],$/m, "it is not one of the four permanent links");
  assert.match(shell, /"\/wealth": "\/analytics"/, "Kekayaan highlights Laporan");
  assert.match(shell, /"\/settings": "\/household"/, "Pengaturan highlights Keluarga");
  assert.match(shell, /user\?\.isSuperAdmin && <Link className="admin-link" href="\/admin"/);
  const provider = text("app/components/InboxCountProvider.js");
  assert.match(provider, /reviewCount: reviews, actionCount: items\[1\]\.length/, "decisions and setup actions are counted apart");
  assert.match(provider, /export function useInboxCounts/);
});

test("admin console uses the shared Tinjauan vocabulary", () => {
  assert.doesNotMatch(tree("app/admin"), /Memuat review|Per jenis review|<h2>Review<\/h2>|"Review"/);
});

test("destructive and text-entry choices use the shared dialog, not window.confirm/prompt", () => {
  for (const file of ["app/components/SettingsPanels.js", "app/household/page.js", "app/admin/", "app/components/CycleDecisions.js"]) {
    const source = text(file);
    assert.doesNotMatch(source, /window\.(confirm|prompt)\(|if \(!confirm\(/, `${file} avoids native dialogs`);
    assert.match(source, /useDialogs/, `${file} uses the shared dialog hook`);
  }
  const dialogs = text("app/components/useDialogs.js");
  assert.match(dialogs, /showModal\(\)/);
  assert.match(dialogs, /onCancel=/);
  assert.match(dialogs, /useId\(\)/);
  assert.doesNotMatch(dialogs, /app-dialog-title/);
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
  assert.match(text("app/layout.js"), /<InboxCountProvider><QuickRecordProvider>\{children\}<\/QuickRecordProvider><\/InboxCountProvider>/);
  assert.match(text("app/inbox/page.js"), /if \(loader\.status === LOAD\.READY && complete\) publishInboxCount\(\{ reviewCount: decisions \}\)/, "the review page never clobbers the action count");
  assert.match(provider, /X-Decision-Count/);
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
  for (const name of ["home", "analytics", "inbox", "household"]) {
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
  assert.match(inbox, /Daftar yang perlu dicek belum dapat dimuat/);
});

test("large screens are split into modules and review cards carry no inline styles", () => {
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
  assert.match(home, /apply\[name\]\(await responses\[index\]\.json\(\), responses\[index\]\)/);

  const shell = text("app/components/AppShell.js");
  const navKeys = [...shell.matchAll(/^\s+\["\/[^"]*", "[^"]+", "(\w+)"\],$/gm)].map(match => match[1]);
  const iconMap = shell.match(/const icons = \{([^}]*)\}/)[1];
  assert.equal(navKeys.length, 4, "four permanent places");
  for (const key of [...navKeys, "inbox", "admin"]) assert.match(iconMap, new RegExp(`\\b${key}: `), `nav icon "${key}" is mapped`);
});

test("a malformed optional list does not hide the reviews", () => {
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /const readOptional = async response => \{ try \{/);
  assert.match(inbox, /if \(reviewList === null\) throw new Error/);
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

test("the category donut does not animate in, so it never captures or shows a half-drawn ring", () => {
  const donut = text("app/components/Charts.js").match(/export function CategoryDonutChart[\s\S]*?\n\}/)?.[0] ?? "";
  assert.match(donut, /<Pie [^>]*isAnimationActive=\{false\}/);
});

test("charts are images with a name, never role=application, and the dashboard has a value table", () => {
  const charts = text("app/components/Charts.js");
  assert.doesNotMatch(charts, /role="application"/, "no chart switches screen readers out of browse mode");
  for (const name of ["DashboardDailySpendingChart", "NetWorthHistoryChart", "CategoryDonutChart", "MonthlyCashflowChart", "CategoryRankingChart"]) {
    const body = charts.match(new RegExp(`export function ${name}[\\s\\S]*?\\n\\}`))?.[0] ?? "";
    assert.match(body, /role="img" aria-label=/, `${name} is a named image`);
  }
  const home = text("app/page.js");
  assert.match(home, /<summary>Lihat nilai pengeluaran harian<\/summary>/, "the dashboard chart has a value table");
  assert.match(home, /<caption className="visually-hidden">Pengeluaran bersih harian<\/caption>/);
});

test("a bar-series legend swatch is a filled square, not a line", () => {
  const charts = text("app/components/Charts.js");
  assert.match(charts, /<ul className="dash-legend" data-shape="bar"/, "the cashflow legend marks its series as bars");
  assert.match(css(), /\.dash-legend\[data-shape="bar"\] i \{[^}]*background: var\(--chart-expense\)/, "the bar swatch is filled");
});

test("the viewport extends under the notch and safe-area insets keep content clear of it", () => {
  assert.match(text("app/layout.js"), /export const viewport = \{[^}]*viewportFit: "cover"/);
  assert.match(css(), /env\(safe-area-inset-left\)/);
  assert.match(css(), /env\(safe-area-inset-bottom\)/);
});

test("loading skeletons say they are loading", () => {
  assert.match(text("app/components/Feedback.js"), /<p className="skeleton-note" aria-hidden="true">\{label\}…<\/p>/);
});

test("transaction filters are inert while their data loads", () => {
  assert.match(text("app/transactions/page.js"), /<form id="transaction-filters" key=\{filterKey\} className="filter-bar" onSubmit=\{filter\} inert=\{loading\}>/);
});

test("document thumbnails load lazily and the list shows twelve at a time", () => {
  const page = text("app/documents/page.js");
  assert.match(page, /alt="Pratinjau dokumen keuangan" loading="lazy" decoding="async"/);
  assert.match(page, /visibleItems\.slice\(0, visibleCount\)\.map/);
  assert.match(page, /setVisibleCount\(count => count \+ 12\)/);
  assert.ok(page.includes("src={`/api/v1/documents/${item.id}/thumbnail`}"));
  assert.ok(page.includes("src={`/api/v1/documents/${selected.id}/pages/${page}/content`}"));
});

test("documents: one pager, an honest status filter, and an upload form that waits for a file", () => {
  const page = text("app/documents/page.js");
  assert.equal((page.match(/Tampilkan lebih banyak/g) || []).length, 1, "a single pagination label");
  assert.doesNotMatch(page, /Muat dokumen lebih lama/);
  assert.match(page, /function showMore\(\)/);
  assert.match(page, /di antara \$\{items\.length\} dokumen termuat/);
  assert.match(page, /Muat lebih banyak untuk memeriksa dokumen yang lebih lama\./);
  assert.match(page, /<button disabled=\{working \|\| !fileInfo\}>/);
  assert.match(page, /JPG atau PNG · maksimal 10 MB total · maksimal 10 gambar<\/small>/);
  assert.doesNotMatch(page, /actions=\{<span className="header-meta">/, "the limits left the page header");
  assert.doesNotMatch(text("app/styles/05-inbox-and-documents.css"), /\.document-card h2 \{[^}]*text-overflow: ellipsis/);
  assert.match(text("app/styles/11-responsive.css"), /@media \(pointer: coarse\)\s*\{[^@]*\.filter-chips \.chip, \.document-more button \{ min-height: 44px; \}/);
});

test("document source and status labels cover the values the API returns", () => {
  const page = text("app/documents/page.js");
  assert.match(page, /WEB_IMAGE: "Unggahan web"/);
  assert.match(page, /EXTRACTED: "Terbaca"/);
});

test("transaction rows are one quiet line at every width; the old table columns are gone", () => {
  const sheet = css();
  assert.doesNotMatch(sheet, /\.table-head|\.transaction-meta|\.transaction-state/, "no leftover table-column rules");
  assert.match(sheet, /\.transaction-row \{ display: grid; grid-template-columns: minmax\(0, 1fr\) auto;/);
  assert.match(sheet, /\.ledger-day-head \{/, "rows group under a day header");
  assert.match(sheet, /\.filter-more\[hidden\] \{ display: none; \}/, "the extra filters collapse");
  assert.doesNotMatch(sheet, /\.filter-bar:not\(\.is-open\)/, "the filter bar itself never hides");
});

test("Beranda keeps one ink-outlined hero; supporting cards are calm", () => {
  const sheet = css();
  assert.match(sheet, /\.home-card \{ border: 1\.5px solid var\(--line-strong\); background: var\(--surface\); box-shadow: none; \}/);
  const home = text("app/page.js");
  assert.equal((home.match(/className="surface home-hero/g) || []).length, 2, "hero and welcome card are the only .surface cards");
  assert.doesNotMatch(home, /Unggah bukti/, "+ Catat owns photo upload");
  assert.match(home, /period=current_cycle/);
  assert.doesNotMatch(home, /last_3_cycles/);
});

test("the visual smoke covers a portrait tablet", () => {
  assert.match(text("scripts/visual-smoke.mjs"), /\["tablet-portrait", 768, 1024\]/);
});

const appSources = dir => readdirSync(new URL(`../${dir}/`, import.meta.url), { withFileTypes: true })
  .flatMap(entry => entry.isDirectory() ? appSources(`${dir}/${entry.name}`) : entry.name.endsWith(".js") ? [`${dir}/${entry.name}`] : []);

test("every scrolling table wrapper takes keyboard focus and has a name", () => {
  const wrappers = appSources("app").flatMap(file => [...text(file).matchAll(/<div className="(?:review-table-wrap|heat-wrap|merchant-rank-scroll)"[^>]*>/g)].map(match => ({ file, tag: match[0] })));
  assert.ok(wrappers.length >= 4, "the table wrappers are found");
  for (const { file, tag } of wrappers) assert.match(tag, /tabIndex=\{0\} role="region" aria-label="[^"]+geser untuk semua kolom"/, `${file}: a scrolling table must be focusable, a region, and named`);
});

test("the extraction text block is focusable and named", () => {
  assert.match(text("app/documents/page.js"), /<pre tabIndex=\{0\} role="region" aria-label="Hasil ekstraksi, geser untuk semua isi">/);
});

test("the visual smoke fails on scrolling regions that keyboard users cannot reach", () => {
  const smoke = text("scripts/visual-smoke.mjs");
  assert.match(smoke, /async function unreachableScrollers\(page\)/);
  assert.match(smoke, /scrolling regions without keyboard access or a name/);
});

test("unfinished sources link to the Tindakan tab, which can show and close them", () => {
  const sections = text("app/analytics/CycleDashboard.js");
  assert.match(sections, /\["\/inbox", "\/inbox\?view=actions", "\/wealth", "\/settings"\]\.includes\(blocker\.action\)/);
  assert.match(text("app/lib/cycleReview.js"), /PROCESSING_INCOMPLETE: \["sumber belum selesai diproses", "Buka Tindakan"\]/);
  assert.match(text("app/lib/reviewInbox.js"), /view === "actions"\) return "\/household\?tab=sumber#tindakan"/);
  assert.match(text("app/components/SettingsPanels.js"), /tindakan/);
});

test("every document type the worker can assign has an Indonesian label", () => {
  const processor = readFileSync(new URL("../../worker/internal/document/processor.go", import.meta.url), "utf8");
  const types = [...new Set(processor.match(/\b(?:RECEIPT|PAYSLIP|BANK_TRANSACTION_SCREENSHOT|TRANSFER_PROOF|EWALLET_SCREENSHOT|BILL_OR_INVOICE|TRANSACTION_HISTORY_SCREENSHOT|WEALTH_OBSERVATION|OTHER_FINANCIAL_DOCUMENT|NON_FINANCIAL_OR_UNSUPPORTED)\b/g))];
  assert.equal(types.length, 10);
  for (const type of types) assert.ok(documentTypeLabels[type], `${type} has no label`);
});

test("settings shows labels, not raw enum values", () => {
  const settings = text("app/components/SettingsPanels.js");
  for (const raw of ["{item.accountType}", "{item.wealthType}", "{item.side}", "{item.usageRole}", "{item.relationship}", "{item.status}"]) {
    assert.ok(!settings.includes(raw), `settings still renders ${raw}`);
  }
});

test("chart ticks follow the 12px type floor", () => {
  assert.doesNotMatch(text("app/components/Charts.js"), /fontSize: (?:[0-9]|1[01])\b/);
});

test("transaction date inputs carry visible labels and the chosen date spelled out", () => {
  const page = text("app/transactions/page.js");
  assert.match(page, /<label className="filter-field filter-date"><span>Dari tanggal<\/span><DateInput name="from" type="date"/);
  assert.match(page, /<label className="filter-field filter-date"><span>Sampai tanggal<\/span><DateInput name="to" type="date"/);
  assert.equal((page.match(/<DateInput /g) || []).length, 2, "the add form's date lives in the + Catat sheet");
  assert.match(text("app/components/QuickRecordDialog.js"), /<DateInput name="transactionAt" type="datetime-local"/);
  const field = text("app/components/DateInput.js");
  assert.match(field, /displayDate\(day\)/);
  assert.match(field, /className="date-hint"/);
  assert.doesNotMatch(field, /hh\/bb/);
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

test("documents: a failed card never claims success, titles are distinguishable, retry and filter exist", () => {
  const page = text("app/documents/page.js");
  assert.match(page, /item\.status === "FAILED" \? <p className="document-summary">/, "failed cards drop the payment summary");
  assert.match(page, /item\.status !== "FAILED" && item\.confidence \? `Keyakinan ekstraksi/, "stored score is document_extraction.confidence, labelled as extraction and hidden on failure");
  assert.doesNotMatch(page, /Tingkat keyakinan/);
  assert.match(page, /function documentTitle\(item\)/);
  assert.match(page, /\/api\/v1\/documents\/\$\{item\.id\}\/reprocess/, "retry uses the Go reprocess endpoint");
  assert.match(page, /Proses ulang/);
  assert.match(page, /FILTERS = \[\["ALL", "Semua"\], \["FAILED", "Gagal"\], \["REVIEW", "Perlu ditinjau"\], \["OK", "Berhasil"\]\]/);
  assert.match(page, /Klasifikasi/);
  assert.doesNotMatch(page, /CLASSIFICATION"?\s*[<}]/, "the raw enum is never rendered");
  assert.match(page, /file-picker-button/, "the native file input is replaced by Indonesian copy");
});

test("documents: titles read screenshot transactions[] and fall back to date and source", () => {
  const page = text("app/documents/page.js");
  const body = page.slice(page.indexOf("function documentTitle"), page.indexOf("function stageLabel"));
  assert.match(body, /summary\?\.transactions/);
  assert.ok(body.includes("/^\\d+(\\.\\d+)?$/.test"), "amount check accepts decimal strings");
  assert.ok(!body.includes("/^d+$/"));
  assert.match(body, /if \(!parts\.length\)/, "a card with no merchant falls back to source and date");
  assert.match(body, /transaksi/);
  const rawFn = new Function("documentTypeLabel", "money", "sourceLabel", "dateTime", `${body}\nreturn documentTitle;`)(v => ({ EWALLET_SCREENSHOT: "Tangkapan layar dompet digital", RECEIPT: "Struk" }[v]), money, () => "Gambar Telegram", () => "15 Nov 2026, 12.48");
  const fn = doc => rawFn(doc).replace(/\s/g, " ");
  const shot = { documentType: "EWALLET_SCREENSHOT", status: "EXTRACTED", summary: { transactions: [{ merchant: "Kopi Kenangan", amount: "45000" }, { merchant: "X", amount: "1" }] } };
  assert.equal(fn(shot), "Kopi Kenangan · Rp 45.000 · 2 transaksi");
  assert.equal(fn({ documentType: "RECEIPT", status: "EXTRACTED", summary: { merchant: "Toko", total: "50000" } }), "Toko · Rp 50.000");
  assert.equal(fn({ documentType: "RECEIPT", status: "EXTRACTED", summary: { merchant: "Toko", total: "45000.00" } }), "Toko · Rp 45.000");
  assert.equal(fn({ documentType: "RECEIPT", status: "EXTRACTED", summary: { merchant: "Toko", total: "abc" } }), "Toko");
  assert.equal(fn({ documentType: "EWALLET_SCREENSHOT", status: "FAILED", summary: { transactions: [{ merchant: "Z" }] } }), "Gambar Telegram 15 Nov 2026, 12.48");
});

test("N7/N9: drawer trap reaches <summary> and skips unrendered controls; document cards use shared labels and money()", () => {
  const hook = text("app/components/useDrawerA11y.js");
  assert.match(hook, /a\[href\], summary, button/, "the trap's focusable selector includes <summary>");
  assert.match(hook, /getClientRects\(\)\.length > 0/, "controls inside a closed <details> are not counted as the last focusable");
  const page = text("app/documents/page.js");
  assert.match(page, /NEEDS_REVIEW: "Perlu ditinjau"/, "the raw NEEDS_REVIEW enum has a label");
  assert.match(page, /item\.needsReview && item\.status !== "NEEDS_REVIEW"/, "no second 'Perlu ditinjau' under the badge");
  const body = page.slice(page.indexOf("function summaryText"), page.indexOf("function statusLabel"));
  const summaryText = new Function("money", `${body}\nreturn summaryText;`)(v => `Rp${v}`);
  assert.equal(summaryText({ merchant: "Indomaret", total: "34000" }), "Indomaret · Rp34000");
  assert.equal(summaryText({ transactions: [{ merchant: "Kopi", amount: "45000" }] }), "Kopi · Rp45000");
});

test("inbox tabs: touch devices get the 44px target without changing the shared desktop rule", () => {
  const sheet = css();
  assert.match(sheet, /\.inbox-tabs button, \.admin-tabs button, \.range-controls button \{ min-height: var\(--control-height-compact\);/, "desktop keeps the compact tab height");
  assert.match(sheet, /@media \(pointer: coarse\) \{\s*\.inbox-tabs button, \.admin-tabs button, \.range-controls button \{ min-height: var\(--control-height\); \}\s*\}/, "inbox, admin and range tabs reach 44px on coarse pointers");
});

test("one card, one card title, one control scale", () => {
  const source = css();
  for (const token of ["card-padding", "card-padding-dense", "card-title-size", "card-title-weight", "control-height", "control-height-compact", "control-font", "control-font-compact"]) {
    assert.match(source, new RegExp(`--${token}: `), `--${token} is missing from the component scale`);
  }
  assert.match(source, /\.surface, \.panel, \.card \{ border: 2px solid var\(--ink\); border-radius: var\(--radius-md\);/, "the card outline is shared");
  assert.match(source, /\.card \{ padding: var\(--card-padding\); \}/, "a standard card uses the card padding token");
  assert.match(source, /\.card--dense \{ padding: var\(--card-padding-dense\); \}/, "a dense card is a named modifier");
  assert.match(source, /\.eyebrow \{[^}]*text-transform: uppercase/, "the eyebrow caps are a style, not typed text");
  assert.match(source, /\.section-title h2[^{]*\{[^}]*font-size: var\(--card-title-size\)/, "card titles share one size");
  assert.match(source, /\.file-picker-button \{[^}]*font-size: var\(--control-font\)/, "the file picker matches the primary button text");
  assert.match(source, /\.merchant-rank th a \{[^}]*min-height: 24px/, "analytics merchant links keep a 24px target");
});

test("eyebrow source strings are sentence case; the caps come from CSS", () => {
  for (const file of appSources("app")) {
    const source = text(file);
    for (const match of source.matchAll(/className="eyebrow">([^<{]+)</g)) {
      assert.doesNotMatch(match[1], /^[A-Z0-9 ·]*[A-Z][A-Z0-9 ·]*$/, `${file}: eyebrow "${match[1]}" is typed in caps`);
    }
    for (const match of source.matchAll(/eyebrow="([^"]+)"/g)) {
      assert.doesNotMatch(match[1], /^[A-Z0-9 ·]*[A-Z][A-Z0-9 ·]*$/, `${file}: eyebrow "${match[1]}" is typed in caps`);
    }
  }
});
