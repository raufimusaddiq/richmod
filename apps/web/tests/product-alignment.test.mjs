import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import test from "node:test";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("all Product Alignment routes exist", () => {
  for (const route of ["page.js", "wealth/page.js", "transactions/page.js", "analytics/page.js", "inbox/page.js", "reviews/page.js", "actions/page.js", "documents/page.js", "household/page.js", "settings/page.js"]) {
    assert.ok(statSync(new URL(`../app/${route}`, import.meta.url)).isFile(), route);
  }
});

test("one inbox exposes separate transaction and integration action views", () => {
  const inbox = text("app/inbox/page.js");
  const shell = text("app/components/AppShell.js");
  assert.match(inbox, /\/api\/v1\/reviews/);
  assert.match(inbox, /\/api\/v1\/integration-actions/);
  assert.match(inbox, />Transaksi <b>/);
  assert.match(inbox, />Tindakan <b>/);
  assert.match(inbox, /Verifikasi penerusan/);
  assert.match(inbox, /noopener noreferrer/);
  assert.match(inbox, /user\?\.household\?\.role === "OWNER"/);
  assert.match(inbox, /Pemilik household perlu menyelesaikan tindakan ini/);
  assert.match(shell, /\["\/inbox", "Inbox", "✓"\]/);
  assert.match(shell, /nav-badge/);
  assert.match(text("app/reviews/page.js"), /redirect\("\/inbox\?view=transactions"\)/);
  assert.match(text("app/actions/page.js"), /redirect\("\/inbox\?view=actions"\)/);
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
  assert.match(styles, /\.insight-card \{ padding: 22px; \}/);
  assert.match(styles, /\.analytics-detail-layout, \.admin-grid \{ display: grid; grid-template-columns: minmax\(0, 1\.55fr\) minmax\(280px, \.75fr\);/);
  assert.match(styles, /@media \(max-width: 1100px\) \{[\s\S]*?\.analytics-detail-layout, \.admin-grid \{ grid-template-columns: minmax\(0, 1fr\); \}/);
});

test("analytics components own semantics, spacing, controls, and chart colors", () => {
  const analytics = text("app/analytics/page.js");
  const charts = text("app/components/Charts.js");
  const styles = text("app/globals.css");
  assert.match(analytics, /className="analytics-flow"/);
  assert.match(analytics, /className="surface analytics-ranked-card"/);
  assert.match(analytics, /<strong>\{value\}<\/strong>/);
  assert.doesNotMatch(analytics, /<b>\{value\}<\/b>/);
  assert.match(analytics, /className="range-control-group"/);
  assert.match(analytics, /className="custom-range"/);
  assert.match(styles, /\.analytics-ranked-card \{ padding: 22px; \}/);
  assert.match(styles, /\.custom-range input \{ width: 142px;/);
  assert.match(charts, /var\(--chart-income\)/);
  assert.match(charts, /var\(--chart-expense\)/);
  assert.doesNotMatch(charts, /#[0-9a-f]{3,8}/i);
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

test("ledger navigation uses the shared Phosphor icon family", () => {
  const source = text("app/components/AppShell.js");
  assert.match(source, /Receipt/);
  assert.match(source, /const icons =/);
  assert.match(source, /ledger: Receipt/);
});

test("settings navigation uses the shared Phosphor icon family", () => {
  const source = text("app/components/AppShell.js");
  assert.match(source, /\["\/settings", "Pengaturan", "settings"\]/);
  assert.match(source, /settings: GearSix/);
  assert.match(source, /<Icon aria-hidden="true"/);
});

test("mobile shell keeps navigation and dense actions usable", () => {
  const shell = text("app/components/AppShell.js");
  const styles = text("app/globals.css");
  assert.match(shell, /aria-modal="true"/);
  assert.match(shell, /event\.key === "Escape"/);
  assert.match(shell, /aria-controls="mobile-more-panel"/);
  assert.match(styles, /\.mobile-nav a, \.mobile-nav button \{[\s\S]*?min-height: 50px;/);
  assert.match(styles, /\.review-actions, \.transfer-options, \.action-buttons, \.dialog-actions, \.row-actions, \.member-actions, \.invite-actions, \.integration-actions \{ display: flex; flex-wrap: wrap;/);
  assert.match(styles, /@media \(max-width: 680px\) \{[\s\S]*?\.member-list article, \.settings-list article, \.integration-grid article \{ grid-template-columns: minmax\(0, 1fr\);/);
  assert.match(styles, /max-height: 82dvh; overflow-y: auto;/);
});

test("email ingress controls stay grouped inside the integration card", () => {
  const settings = text("app/settings/page.js");
  const styles = text("app/globals.css");
  assert.match(settings, /className="integration-actions"/);
  assert.match(styles, /\.review-actions, \.transfer-options, \.action-buttons, \.dialog-actions, \.row-actions, \.member-actions, \.invite-actions, \.integration-actions \{ display: flex; flex-wrap: wrap;/);
  assert.match(styles, /\.integration-grid small \{ margin-top: 3px; color: var\(--muted\); font-size: 10px; \}/);
});

test("web and Telegram share the same review object endpoint", () => {
  assert.match(text("app/inbox/page.js"), /\/api\/v1\/reviews/);
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
  assert.match(styles, /prefers-reduced-motion: reduce/);
  assert.match(styles, /\.transaction-row \{ width: 100%;/);
  assert.match(styles, /:focus-visible/);
  assert.match(styles, /--accent: #6d435c/);
  assert.match(styles, /--income: #216247/);
  assert.match(styles, /\.app-main \{ width: calc\(100% - var\(--sidebar\)\)/);
  assert.doesNotMatch(styles, /\.app-main \{ width: min\(1440px/);
  assert.match(styles, /button\.document-card:hover:not\(:disabled\) \{ border-color: var\(--line-strong\); background: var\(--surface-muted\); color: var\(--ink\); \}/);
});

test("public routes preserve the authenticated overview and dedicated login flow", () => {
  const home = text("app/page.js");
  const landing = text("app/components/LandingPage.js");
  const login = text("app/login/page.js");
  const publicShell = text("app/components/PublicShell.js");
  assert.match(home, /if \(user === false\) return <LandingPage/);
  assert.match(home, /return <AppShell user=\{user\}/);
  assert.match(landing, /Keuangan keluarga,/);
  assert.match(landing, /Richmod bertanya—bukan mengarang/);
  assert.match(landing, /Review Inbox/);
  assert.match(landing, /richmod-evidence-flow\.svg/);
  assert.match(landing, /Alur Richmod dari bukti ke ledger atau keputusan manusia/);
  assert.doesNotMatch(landing, /function EvidenceBoard/);
  assert.match(login, /useAuth\(false\)/);
  assert.match(login, /window\.location\.replace\("\/"\)/);
  assert.match(login, /\/api\/v1\/auth\/login/);
  assert.match(login, /<PublicNav hideLogin \/>/);
  assert.match(login, /richmod-login-treeline\.svg/);
  assert.match(login, /alt="" aria-hidden="true"/);
  assert.match(publicShell, /href="\/#cara-kerja"/);
  assert.match(publicShell, /href="\/#kepercayaan"/);
  assert.match(publicShell, /\{!hideLogin && <Link className="public-nav-login"/);
  assert.match(landing, /Richmod memeriksa hasilnya dengan aturan yang konsisten/);
  assert.doesNotMatch(landing, /Go memvalidasi fakta secara deterministik/);
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
  assert.match(styles, /\.admin-table thead \{ display: none; \}/);
  assert.match(styles, /\.admin-table td::before \{ color: var\(--muted\); content: attr\(data-label\);/);
  assert.match(styles, /\.detail-drawer, \.admin-drawer \{ padding: 20px 16px 96px; border-left: 0; \}/);
  assert.match(styles, /\.mobile-more-header button \{ display: grid; width: 38px;/);
  assert.match(styles, /\.admin-table \{ min-width: 0; border-collapse: separate; border-spacing: 0 10px; \}/);
  assert.match(styles, /\.admin-table tr \{ padding: 12px 14px;/);
  assert.match(styles, /\.admin-table td \{ display: grid; grid-template-columns: minmax\(100px, \.38fr\) minmax\(0, 1fr\); gap: 8px; padding: 5px 0; border: 0;/);
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
