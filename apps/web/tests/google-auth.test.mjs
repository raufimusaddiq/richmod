import assert from "node:assert/strict";
import test from "node:test";
import { GOOGLE_INVITE_START_PATH, googleLoginErrorMessage, googleStartHref } from "../app/lib/authErrors.js";

test("googleLoginErrorMessage maps every known code to Indonesian copy", () => {
  assert.match(googleLoginErrorMessage("invite_required"), /menunggu aktivasi undangan/);
  assert.match(googleLoginErrorMessage("google_email_mismatch"), /Email Google tidak cocok/);
  assert.match(googleLoginErrorMessage("invite_invalid"), /kedaluwarsa atau tidak berlaku/);
  assert.equal(googleLoginErrorMessage("google_denied"), "Masuk dengan Google dibatalkan.");
  assert.match(googleLoginErrorMessage("google_unavailable"), /belum tersedia/);
  assert.equal(googleLoginErrorMessage("google_failed"), "Masuk dengan Google belum berhasil. Coba lagi.");
});

test("googleLoginErrorMessage falls back for unknown codes and stays empty for none", () => {
  assert.equal(googleLoginErrorMessage("surprise"), "Tidak dapat masuk saat ini. Coba lagi.");
  assert.equal(googleLoginErrorMessage("constructor"), "Tidak dapat masuk saat ini. Coba lagi.");
  assert.equal(googleLoginErrorMessage(""), "");
  assert.equal(googleLoginErrorMessage(null), "");
  assert.equal(googleLoginErrorMessage(undefined), "");
});

test("googleStartHref never embeds an invite token in the GET URL", () => {
  assert.equal(googleStartHref(), "/api/v1/auth/google/start");
  assert.equal(googleStartHref("abc-DEF_123"), "/api/v1/auth/google/start");
  assert.equal(GOOGLE_INVITE_START_PATH, "/api/v1/auth/google/invite");
});
