import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import test from "node:test";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("all Product Alignment routes exist", () => {
  for (const route of ["page.js", "transactions/page.js", "analytics/page.js", "reviews/page.js", "documents/page.js", "household/page.js", "settings/page.js"]) {
    assert.ok(statSync(new URL(`../app/${route}`, import.meta.url)).isFile(), route);
  }
});

test("active frontend contains no budget requests or budget interface", () => {
  const files = ["app/page.js", "app/components/AppShell.js", "app/settings/page.js"];
  const source = files.map(text).join("\n").toLowerCase();
  assert.equal(source.includes("/api/v1/budgets"), false);
  assert.equal(source.includes("anggaran bulanan"), false);
});

test("overview chart is backed by deterministic analytics API", () => {
  assert.match(text("app/page.js"), /analytics\/cashflow\?range=12/);
  assert.match(text("app/components/Charts.js"), /recharts/);
  assert.match(text("app/components/Charts.js"), /ResponsiveContainer/);
});

test("transaction filters are query-backed", () => {
  const source = text("app/transactions/page.js");
  for (const name of ["from", "to", "type", "categoryId", "memberId", "status", "accountId", "source", "q"]) assert.match(source, new RegExp(`name=\\"${name}\\"`));
  assert.match(source, /URLSearchParams/);
});

test("ledger request failures are recoverable instead of leaving the page loading", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /catch \{/);
  assert.match(source, /Koneksi terputus saat memuat ledger/);
  assert.match(source, /<ErrorNotice message=\{error\} retry=\{load\}\/>/);
  assert.match(source, /loading \? <Skeleton/);
  assert.match(source, /dynamic = "force-dynamic"/);
  assert.match(text("app/components/useAuth.js"), /catch\(unavailable\)/);
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

test("web and Telegram share the same review object endpoint", () => {
  assert.match(text("app/reviews/page.js"), /\/api\/v1\/reviews/);
  assert.match(text("app/components/ReviewCards.js"), /classify-transfer/);
  assert.match(text("app/components/ReviewCards.js"), /transactions\?id=/);
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
