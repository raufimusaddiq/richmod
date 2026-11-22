import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import { component } from "./support/component.mjs";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";

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

test("Perlu dicek no longer hosts Email masuk; the old deep link moves to Keluarga", () => {
  const inbox = read("app/inbox/page.js");
  assert.doesNotMatch(inbox, /email-ingress\/deliveries/);
  assert.doesNotMatch(inbox, /data-view="email"/);
  assert.match(inbox, /legacyViewTarget/);
});

test("Keluarga > Sumber otomatis hosts the owner delivery log and pending actions", () => {
  const settings = read("app/components/SettingsPanels.js");
  assert.doesNotMatch(settings, /Pengiriman terakhir/);
  assert.doesNotMatch(settings, /emailDelivery(Status|Reason)Label/);
  assert.match(settings, /<EmailOnboardingChecklist /);
  assert.match(settings, /Status pemrosesan otomatis/);
  assert.match(settings, /<DeliveryLogSection tab="sumber" active=\{tab\}\/>/);
  assert.match(settings, /<IntegrationActionsSection tab="sumber"/);
  assert.match(settings, /title="Email masuk terakhir"/);
  assert.match(settings, /title="Tindakan"/);
  assert.match(settings, /\/api\/v1\/integrations\/email-ingress\/deliveries/);
  assert.match(settings, /\/api\/v1\/integration-actions\/\$\{id\}\/resolve/);
  assert.match(settings, /if \(!owner\) return <NotificationsSection/);
});

test("the delivery list is height-bounded", () => {
  const css = read("app/styles/05-inbox-and-documents.css");
  assert.match(css, /\.delivery-list \{[^}]*max-height:[^}]*overflow-y: auto/);
});
