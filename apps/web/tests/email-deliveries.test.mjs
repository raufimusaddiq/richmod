import assert from "node:assert/strict";
import test from "node:test";
import { component } from "./support/component.mjs";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";

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
