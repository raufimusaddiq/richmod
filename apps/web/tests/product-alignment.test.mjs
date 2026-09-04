import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import test from "node:test";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("all Product Alignment routes exist", () => {
  for (const route of ["page.js", "transactions/page.js", "analytics/page.js", "reviews/page.js", "actions/page.js", "documents/page.js", "household/page.js", "settings/page.js"]) {
    assert.ok(statSync(new URL(`../app/${route}`, import.meta.url)).isFile(), route);
  }
});

test("integration actions stay separate from financial reviews", () => {
  const actions = text("app/actions/page.js");
  const shell = text("app/components/AppShell.js");
  assert.match(actions, /\/api\/v1\/integration-actions/);
  assert.match(actions, /Verifikasi penerusan/);
  assert.match(actions, /noopener noreferrer/);
  assert.match(shell, /\["\/actions", "Tindakan", "!"\]/);
  assert.match(shell, /nav-badge/);
});

test("active frontend contains no budget requests or budget interface", () => {
  const files = ["app/page.js", "app/components/AppShell.js", "app/settings/page.js"];
  const source = files.map(text).join("\n").toLowerCase();
  assert.equal(source.includes("/api/v1/budgets"), false);
  assert.equal(source.includes("anggaran bulanan"), false);
});

test("overview chart is backed by deterministic analytics API", () => {
  const source = text("app/page.js");
  assert.match(source, /analytics\/cycle\/daily/);
  assert.match(source, /transactions\?limit=8/);
  assert.match(text("app/components/Charts.js"), /recharts/);
  assert.match(text("app/components/Charts.js"), /ResponsiveContainer/);
});

test("charts answer distinct dashboard, cycle, calendar, and category questions", () => {
  const charts = text("app/components/Charts.js");
  const home = text("app/page.js");
  const analytics = text("app/analytics/page.js");
  for (const name of ["DashboardDailySpendingChart", "CycleSpendingPatternChart", "MonthlyCashflowChart", "CategoryDonutChart", "CategoryRankingChart"]) assert.match(charts, new RegExp(`export function ${name}`));
  assert.match(home, /DashboardDailySpendingChart/);
  assert.match(home, /CategoryDonutChart/);
  assert.match(analytics, /CycleSpendingPatternChart/);
  assert.match(analytics, /MonthlyCashflowChart/);
  assert.match(analytics, /CategoryRankingChart/);
  assert.doesNotMatch(charts, /cumulativeValue|AreaChart|<Line/);
  assert.match(charts, /ReferenceLine/);
});

test("analytics insight UI is aggregate-only and safely rendered", () => {
  const analytics = text("app/analytics/page.js");
  const card = text("app/components/InsightCard.js");
  assert.match(analytics, /api\/v1\/insights/);
  assert.match(analytics, /generate\?period=cycle/);
  assert.match(analytics, /pollInsight/);
  assert.match(card, /split\(\/\\n\{2,\}\//);
  assert.doesNotMatch(card, /dangerouslySetInnerHTML/);
  assert.ok(analytics.indexOf("analytics-kpis") < analytics.indexOf("analytics-chart"));
  assert.ok(analytics.indexOf("analytics-chart") < analytics.indexOf("<InsightCard"));
  assert.ok(analytics.indexOf("<InsightCard") < analytics.indexOf("analytics-detail-layout"));
  assert.match(card, /insight-card-compact/);
  assert.match(card, /insight-state-copy/);
});

test("analytics insight card owns its spacing", () => {
  const styles = text("app/globals.css");
  assert.match(styles, /\.insight-card\{[^}]*margin-top:14px[^}]*padding:20px/);
  assert.match(styles, /\.insight-card-compact\{[^}]*padding:15px 18px/);
  assert.match(styles, /\.insight-card>\.section-title\{margin-bottom:0\}/);
  assert.match(styles, /\.analytics-detail-layout\{[^}]*1\.55fr[^}]*\.85fr/);
  assert.match(styles, /@media\(max-width:900px\)\{\.analytics-detail-layout\{grid-template-columns:1fr\}/);
});

test("transaction filters are query-backed", () => {
  const source = text("app/transactions/page.js");
  for (const name of ["from", "to", "type", "categoryId", "memberId", "status", "accountId", "source", "q"]) assert.match(source, new RegExp(`name=\\"${name}\\"`));
  assert.match(source, /URLSearchParams/);
});

test("ledger request failures are recoverable instead of leaving the page loading", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /catch \{/);
  assert.match(source, /Koneksi terputus saat memuat riwayat transaksi/);
  assert.match(source, /<ErrorNotice message=\{error\} retry=\{load\}\/>/);
  assert.match(source, /loading \? <Skeleton/);
  assert.match(source, /dynamic = "force-dynamic"/);
  const auth = text("app/components/AuthProvider.js");
  assert.match(auth, /catch \{/);
  assert.match(auth, /setLoaded\(true\)/);
});

test("manual transactions use an accessible dialog and refresh the ledger", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /<dialog/);
  assert.match(source, /aria-labelledby="create-transaction-title"/);
  assert.match(source, /fetch\("\/api\/v1\/transactions", \{ method: "POST"/);
  assert.match(source, /await load\(\)/);
});

test("authenticated app shells are not cached across deployments", () => {
  const source = text("next.config.mjs");
  assert.match(source, /Cache-Control/);
  assert.match(source, /no-store/);
  assert.match(source, /\/transactions/);
});

test("ledger navigation uses a scalable icon instead of a text glyph", () => {
  const source = text("app/components/AppShell.js");
  assert.match(source, /function LedgerIcon/);
  assert.match(source, /<svg viewBox="0 0 24 24"/);
  assert.equal(source.includes('"↕"'), false);
});

test("settings navigation uses the shared scalable icon style", () => {
  const source = text("app/components/AppShell.js");
  assert.match(source, /\["\/settings", "Pengaturan", "settings"\]/);
  assert.match(source, /function SettingsIcon/);
  assert.match(source, /icon === "settings" \? <SettingsIcon\/>/);
  assert.equal(source.includes('"⚙"'), false);
});

test("mobile shell keeps navigation and dense actions usable", () => {
  const shell = text("app/components/AppShell.js");
  const styles = text("app/globals.css");
  assert.match(shell, /aria-modal="true"/);
  assert.match(shell, /event\.key === "Escape"/);
  assert.match(shell, /aria-controls="mobile-more-panel"/);
  assert.match(styles, /\.mobile-nav a,\.mobile-nav button\{min-height:48px/);
  assert.match(styles, /\.page-actions,\.invite-actions,\.member-actions,\.row-actions,\.review-actions,\.transfer-options,\.dialog-actions\{flex-wrap:wrap/);
  assert.match(styles, /\.settings-list article\{grid-template-columns:minmax\(0,1fr\)\}/);
  assert.match(styles, /max-height:min\(82dvh,680px\);overflow-y:auto/);
});

test("web and Telegram share the same review object endpoint", () => {
  assert.match(text("app/reviews/page.js"), /\/api\/v1\/reviews/);
  assert.match(text("app/components/ReviewCards.js"), /classify-transfer/);
  assert.match(text("app/components/ReviewCards.js"), /transactions\?id=/);
  assert.match(text("app/components/ReviewCards.js"), /missingFields\?\.includes\("merchant"\)/);
  assert.match(text("app/components/ReviewCards.js"), /name="merchantName" required/);
});

test("household route exposes Telegram connection state", () => {
  const source = text("app/household/page.js");
  assert.match(source, /telegramConnected/);
  assert.match(source, /telegram-invite/);
});

test("shared UX feedback is accessible and motion respects user preference", () => {
  const feedback = text("app/components/Feedback.js");
  const styles = text("app/globals.css");
  assert.match(feedback, /aria-busy="true"/);
  assert.match(feedback, /role="alert"/);
  assert.match(feedback, /aria-live="polite"/);
  assert.match(styles, /prefers-reduced-motion:reduce/);
  assert.match(styles, /transaction-table \.transaction-row/);
  assert.match(styles, /:focus-visible/);
});

test("admin console keeps platform tabs and redacts sensitive payloads", () => {
  const admin = text("app/admin/page.js");
  for (const label of ["overview", "jobs", "llm", "logs", "households", "users", "audit"]) assert.match(admin, new RegExp(`"${label}"`));
  assert.match(admin, /\/api\/v1\/admin\/overview/);
  assert.match(admin, /\/api\/v1\/admin\/jobs/);
  assert.match(admin, /\/api\/v1\/admin\/llm\/summary/);
  assert.match(admin, /\/api\/v1\/admin\/logs/);
  assert.match(text("../api/cmd/api/main.go"), /admin\/audit\/all/);
  assert.doesNotMatch(admin, /payload_json|last_error|prompt text|raw model output/);
});

test("admin lists use bounded server filters and accessible detail actions", () => {
  const admin = text("app/admin/page.js");
  assert.match(admin, /useAdminList/);
  assert.match(admin, /nextCursor/);
  assert.match(admin, /Muat berikutnya/);
  assert.match(admin, /aria-label="Status job"/);
  assert.match(admin, /aria-label="Reference ID"/);
  assert.match(admin, /admin-link admin-id/);
});

test("admin console adapts tables and drawer for mobile", () => {
  const admin = text("app/admin/page.js");
  const styles = text("app/globals.css");
  assert.match(admin, /Children, cloneElement, isValidElement/);
  assert.match(admin, /"data-label": headers\[index\]/);
  assert.match(styles, /\.admin-table thead\{display:none\}/);
  assert.match(styles, /\.admin-table td::before\{content:attr\(data-label\)/);
  assert.match(styles, /\.admin-drawer\{width:100%;padding:20px 16px 96px;border-left:0\}/);
  assert.match(styles, /\.mobile-more-header button\{display:grid;place-items:center;padding:0;line-height:1\}/);
  assert.match(styles, /\.admin-table\{min-width:0;border-collapse:separate;border-spacing:0 12px\}/);
  assert.match(styles, /\.admin-table tr\{padding:13px 15px/);
  assert.match(styles, /\.admin-table td\{display:grid;grid-template-columns:minmax\(92px,.42fr\) minmax\(0,1fr\);gap:8px;padding:5px 0;border:0;line-height:1\.35/);
  assert.match(styles, /\.admin-table tr\{padding:12px 13px/);
  assert.match(styles, /\.admin-table td\{grid-template-columns:minmax\(88px,.4fr\) minmax\(0,1fr\);gap:7px;padding:4px 0\}/);
});

test("admin audit defaults to combined bounded feed while retaining scoped views", () => {
  const admin = text("app/admin/page.js");
  assert.match(admin, /useState\("all"\)/);
  assert.match(admin, /\/api\/v1\/admin\/audit\/all/);
  assert.match(admin, /<option value="all">Semua<\/option>/);
  assert.match(admin, /<option value="platform">Platform<\/option>/);
  assert.match(admin, /<option value="household">Household<\/option>/);
});

test("admin user changes require confirmation", () => {
  const admin = text("app/admin/page.js");
  assert.match(admin, /confirm\(/);
  assert.match(admin, /ADMINISTRASI PLATFORM/);
  assert.match(text("app/globals.css"), /admin-table-wrap/);
});
