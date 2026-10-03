import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import test from "node:test";
import { reviewCards, tree, globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("all product routes exist", () => {
  for (const route of ["page.js", "wealth/page.js", "transactions/page.js", "analytics/page.js", "inbox/page.js", "reviews/page.js", "actions/page.js", "documents/page.js", "household/page.js", "settings/page.js"]) {
    assert.ok(statSync(new URL(`../app/${route}`, import.meta.url)).isFile(), route);
  }
});

test("one inbox exposes separate transaction and integration action views", () => {
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /\/api\/v1\/reviews/);
  assert.match(inbox, /\/api\/v1\/integration-actions/);
  assert.match(inbox, /noopener noreferrer/);
  assert.match(inbox, /user\?\.household\?\.role === "OWNER"/);
  assert.match(text("app/reviews/page.js"), /redirect\("\/inbox\?view=transactions"\)/);
  assert.match(text("app/actions/page.js"), /redirect\("\/inbox\?view=actions"\)/);
});

test("overview chart is backed by deterministic analytics API", () => {
  assert.match(text("app/page.js"), /analytics\/cycle\/daily/);
});

test("charts answer distinct dashboard, cycle, calendar, and category questions", () => {
  const charts = text("app/components/Charts.js");
  const home = text("app/page.js");
  const analytics = tree("app/analytics");
  for (const name of ["DashboardDailySpendingChart", "CycleSpendingPatternChart", "MonthlyCashflowChart", "CategoryDonutChart", "CategoryRankingChart"]) assert.match(charts, new RegExp(`export function ${name}`));
  assert.match(home, /DashboardDailySpendingChart/);
  assert.match(home, /CategoryDonutChart/);
  assert.match(analytics, /CycleSpendingPatternChart/);
  assert.match(analytics, /MonthlyCashflowChart/);
  assert.match(analytics, /CategoryRankingChart/);
});

test("analytics commentary is selected-cycle prose, safely rendered after deterministic evidence", () => {
  const analytics = tree("app/analytics");
  const card = text("app/components/InsightCard.js");
  assert.match(analytics, /api\/v1\/insights/);
  assert.match(analytics, /pollInsight/);
  assert.doesNotMatch(card, /dangerouslySetInnerHTML/);
  const ordered = ["<CyclePosition", 'id="spending-shape"', 'id="changes"', 'id="drivers"', 'id="destinations"', 'id="household"', "<SavingsWealth", "<QualitySection", 'id="discussion"', "<InsightCard"];
  for (let index = 1; index < ordered.length; index += 1) assert.ok(analytics.indexOf(ordered[index - 1]) < analytics.indexOf(ordered[index]), ordered[index]);
});

test("analytics controls are labelled and charts use theme colour tokens", () => {
  const analytics = tree("app/analytics");
  const charts = text("app/components/Charts.js");
  assert.match(analytics, /aria-labelledby="changes-title"/);
  assert.match(analytics, /aria-controls="category-drivers"/);
  assert.match(charts, /var\(--chart-income\)/);
  assert.match(charts, /var\(--chart-expense\)/);
  assert.doesNotMatch(charts, /#[0-9a-f]{3,8}/i);
});

test("transaction filters are query-backed", () => {
  const source = text("app/transactions/page.js");
  for (const name of ["from", "to", "type", "categoryId", "memberId", "status", "accountId", "source", "q"]) assert.match(source, new RegExp(`name=\\"${name}\\"`));
  assert.match(source, /URLSearchParams/);
  assert.match(source, /window\.history\.pushState/);
  assert.match(source, /addEventListener\("popstate", onPopState\)/);
});

test("ledger request failures are recoverable instead of leaving the page loading", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /<ErrorNotice message=\{error\} retry=\{load\}\/>/);
  assert.match(source, /dynamic = "force-dynamic"/);
  assert.match(text("app/components/AuthProvider.js"), /setLoaded\(true\)/);
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
  assert.match(source, /no-store/);
  assert.match(source, /\/transactions/);
});

test("mobile shell keeps navigation usable and dismissable", () => {
  const shell = text("app/components/AppShell.js");
  assert.match(shell, /aria-modal="true"/);
  assert.match(shell, /event\.key === "Escape"/);
  assert.match(shell, /aria-controls="mobile-more-panel"/);
  assert.match(shell, /<Icon aria-hidden="true"/);
});

test("web and Telegram share the same review object endpoint", () => {
  assert.match(text("app/inbox/page.js"), /\/api\/v1\/reviews/);
  assert.match(reviewCards(), /classify-transfer/);
  assert.match(reviewCards(), /transactions\?id=/);
  assert.match(reviewCards(), /name="merchantName" required/);
});

test("household route exposes Telegram connection state", () => {
  const source = text("app/household/page.js");
  assert.match(source, /telegramConnected/);
  assert.match(source, /telegram-invite/);
});

test("shared UX feedback is accessible and motion respects user preference", () => {
  const feedback = text("app/components/Feedback.js");
  const styles = globalCss();
  assert.match(feedback, /aria-busy="true"/);
  assert.match(feedback, /role="alert"/);
  assert.match(feedback, /aria-live="polite"/);
  assert.match(styles, /prefers-reduced-motion: reduce/);
  assert.match(styles, /:focus-visible/);
});

test("public routes keep the authenticated overview and a dedicated login flow", () => {
  const home = text("app/page.js");
  const login = text("app/login/page.js");
  assert.match(home, /if \(user === false\) return <LandingPage/);
  assert.match(home, /return <AppShell user=\{user\}/);
  assert.match(login, /useAuth\(false\)/);
  assert.match(login, /\/api\/v1\/auth\/login/);
  assert.match(login, /alt="" aria-hidden="true"/);
});

test("admin console keeps platform tabs and redacts sensitive payloads", () => {
  const admin = tree("app/admin");
  for (const label of ["overview", "jobs", "llm", "logs", "households", "users", "audit"]) assert.match(admin, new RegExp(`"${label}"`));
  assert.match(admin, /\/api\/v1\/admin\/audit\/all/);
  assert.doesNotMatch(admin, /payload_json|last_error|prompt text|raw model output/);
});

test("admin lists use bounded server filters and accessible detail actions", () => {
  const admin = tree("app/admin");
  assert.match(admin, /nextCursor/);
  assert.match(admin, /aria-label="Status tugas"/);
  assert.match(admin, /aria-label="Reference ID"/);
});

test("admin tables keep their column labels when stacked on phones", () => {
  assert.match(tree("app/admin"), /"data-label": headers\[index\]/);
  assert.match(globalCss(), /\.admin-table td::before \{[^}]*content: attr\(data-label\);/);
});

test("admin user changes require confirmation", () => {
  assert.match(tree("app/admin"), /confirm\(/);
});

test("settings lists are bounded with pagination", () => {
  const settings = text("app/settings/page.js");
  for (const dataset of ["accounts", "wealthAccounts", "salarySources", "known", "listeners", "financialSources", "categories", "aliases"]) {
    assert.match(settings, new RegExp(`<BoundedList items=\\{data\\.${dataset}\\}`), dataset);
  }
  assert.doesNotMatch(settings, /settings-list">\{data\.[A-Za-z]+\.map/);
});

test("settings sections are labelled landmarks", () => {
  const settings = text("app/settings/page.js");
  assert.match(settings, /aria-labelledby=\{`\$\{id\}-title`\}/);
  assert.match(settings, /<h2 id=\{`\$\{id\}-title`\}>\{title\}<\/h2>/);
  assert.match(settings, /aria-label="Bagian pengaturan"/);
});
