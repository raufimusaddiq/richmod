// The email delivery log lives in the Inbox (owner-only), not in Settings.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";
import swc from "next/dist/build/swc/index.js";

const require = createRequire(import.meta.url);
await swc.loadBindings();
const cache = new Map();
function component(filename) {
  const absolute = path.resolve(filename);
  if (cache.has(absolute)) return cache.get(absolute).exports;
  const module = { exports: {} };
  cache.set(absolute, module);
  const { code } = swc.transformSync(fs.readFileSync(absolute, "utf8"), {
    filename: absolute, jsc: { parser: { syntax: "ecmascript", jsx: true }, transform: { react: { runtime: "automatic" } } }, module: { type: "commonjs" },
  });
  new Function("require", "module", "exports", code)(name => name.startsWith(".") ? component(path.resolve(path.dirname(absolute), name.endsWith(".js") ? name : `${name}.js`)) : require(name), module, module.exports);
  return module.exports;
}
const read = file => fs.readFileSync(file, "utf8");

const EmailDeliveries = component("app/components/EmailDeliveries.js").default;
const render = state => renderToStaticMarkup(React.createElement(EmailDeliveries, { state, retry: () => {} }));

test("deliveries show the Indonesian status, reason, sender and time", () => {
  const html = render({ status: "ready", items: [
    { status: "INGESTED", reason: "", sender: "alert@bank.example", receivedAt: "2026-11-10T03:00:00Z" },
    { status: "IGNORED_AUTH", reason: "UNTRUSTED_AUTHENTICATION", sender: "", receivedAt: "2026-11-10T04:00:00Z" },
    { status: "DUPLICATE", reason: "SOMETHING_NEW", sender: "x@y.example", receivedAt: "2026-11-10T05:00:00Z" },
  ] });
  assert.match(html, /Diproses/);
  assert.match(html, /alert@bank\.example/);
  assert.match(html, /Diabaikan/);
  assert.match(html, /Pengirim tidak diketahui/);
  assert.match(html, /Pengirim tidak lolos verifikasi autentikasi email\./);
  assert.match(html, /SOMETHING_NEW/, "an unknown reason keeps its own code");
  assert.match(html, /10 Nov 2026/);
});

test("empty, loading and error states are short Indonesian messages", () => {
  assert.match(render({ status: "ready", items: [] }), /Belum ada email masuk/);
  assert.match(render({ status: "loading", items: [] }), /Memuat/);
  assert.match(render({ status: "idle", items: [] }), /Memuat/);
  const failed = render({ status: "error", items: [] });
  assert.match(failed, /role="alert"/);
  assert.match(failed, /Coba lagi/);
});

test("the Inbox has an owner-only Email masuk tab fed by the existing endpoint", () => {
  const inbox = read("app/inbox/page.js");
  assert.match(inbox, /\/api\/v1\/integrations\/email-ingress\/deliveries/);
  assert.match(inbox, /\{owner && <button role="tab" data-view="email"/);
  assert.match(inbox, /Email masuk<\/button>/);
  assert.match(inbox, /const view = rawView === "email" && !owner \? "transactions" : rawView/, "a non-owner never reaches the email view");
  assert.match(inbox, /const emailOpen = view === "email";/, "the endpoint is only fetched while the owner has the tab open");
  assert.match(inbox, /owner \? \["transactions", "actions", "history", "email"\]/, "arrow keys skip the tab for non-owners");
});

test("Settings no longer renders or fetches the delivery list", () => {
  const settings = read("app/settings/page.js");
  assert.doesNotMatch(settings, /Pengiriman terakhir/);
  assert.doesNotMatch(settings, /email-ingress\/deliveries/);
  assert.doesNotMatch(settings, /emailDelivery(Status|Reason)Label/);
  assert.match(settings, /<EmailOnboardingChecklist /);
  assert.match(settings, /Worker & LLM gateway/);
});

test("the delivery list is height-bounded", () => {
  const css = read("app/styles/05-inbox-and-documents.css");
  assert.match(css, /\.delivery-list \{[^}]*max-height:[^}]*overflow-y: auto/);
});
