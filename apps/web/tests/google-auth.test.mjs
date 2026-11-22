import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { GOOGLE_INVITE_START_PATH, googleLoginErrorMessage, googleStartHref } from "../app/lib/authErrors.js";
import { globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

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

test("login page offers Google beside the password form and reads ?error=", () => {
  const login = text("app/login/page.js");
  assert.match(login, /className="button public-button-google" href=\{googleStartHref\(\)\}/);
  assert.match(login, /Masuk dengan Google/);
  assert.match(login, /className="login-divider"[^>]*><span>atau dengan email<\/span>/);
  assert.match(login, /googleLoginErrorMessage\(new URLSearchParams\(window\.location\.search\)\.get\("error"\)\)/);
  assert.match(login, /id="login-email"[^>]*enterKeyHint="next"/);
  assert.match(login, /id="login-password"[^>]*enterKeyHint="go"/);
  assert.match(login, /Masuk ke Richmod/);
});

test("invite page posts the hash token to GOOGLE_INVITE_START_PATH before navigating to googleStartHref", () => {
  const invite = text("app/invite/page.js");
  assert.match(invite, /window\.location\.hash\.slice\(1\)/);
  assert.match(invite, /fetch\(GOOGLE_INVITE_START_PATH,\s*\{\s*method:\s*"POST"/);
  assert.match(invite, /JSON\.stringify\(\{\s*token\s*\}\)/);
  assert.match(invite, /window\.location\.hash = "";/);
  assert.match(invite, /window\.location\.href = googleStartHref\(\);/);
  assert.match(invite, /className="button public-button-google"/);
  assert.match(invite, /Aktifkan dengan Google/);
  assert.match(invite, /<span>atau buat kata sandi<\/span>/);
  assert.match(invite, /minLength="12"/);
  assert.match(invite, /Tautan atau kata sandi tidak valid\./);
});

test("NoHouseholdIntro shows the waiting state and its three actions", () => {
  const intro = text("app/components/NoHouseholdIntro.js");
  assert.match(intro, /RUANG KELUARGA BELUM AKTIF/);
  assert.match(intro, /Akun kamu sudah terdaftar\./);
  assert.match(intro, /user\.displayName/);
  assert.match(intro, /user\.email/);
  assert.match(intro, /\/invite#/);
  assert.match(intro, /Periksa status/);
  assert.match(intro, /const \{ reload \} = useAuthContext\(\)/);
  assert.match(intro, /user\.isSuperAdmin && <Link[^>]*href="\/admin"[^>]*>[^]*Buka Konsol Admin/);
  assert.match(intro, /\/api\/v1\/auth\/logout/);
});

test("AuthProvider exposes reload and household-scoped fetches wait for a household", () => {
  const provider = text("app/components/AuthProvider.js");
  assert.match(provider, /reload/);
  assert.match(provider, /value=\{\{ user, loaded, unavailable, reload \}\}/);
  assert.match(text("app/components/InboxCountProvider.js"), /const signedIn = Boolean\(user\?\.household\);/);
  assert.match(text("app/components/CategoryPaletteProvider.js"), /const signedIn = Boolean\(user\?\.household\);/);
  const home = text("app/page.js");
  assert.match(home, /if \(!user \|\| !user\.household\) return;/);
  assert.match(home, /if \(!user\.household\) return <NoHouseholdIntro user=\{user\} \/>;/);
  assert.match(text("app/components/AppShell.js"), /user && !user\.household && !pathname\.startsWith\("\/admin"\)\) return <NoHouseholdIntro/);
});

test("Keluarga member notes and Admin users table distinguish Google-linked accounts from inactive ones", () => {
  const household = text("app/household/page.js");
  assert.match(household, /member\.googleLinked \? "Terhubung dengan Google" : "Belum mengaktifkan akun"/);
  const adminUsers = text("app/admin/Users.js");
  assert.match(adminUsers, /user\.googleLinked/);
  assert.match(adminUsers, /"Diatur · Google"/);
});

test("Google and waiting-screen styles obey the Retro Ledger floor", () => {
  const css = globalCss();
  assert.match(css, /\.public-button-google \{[^}]*border-color: var\(--ink\)/);
  assert.match(css, /\.login-divider \{/);
  assert.match(css, /\.no-household-card \{[^}]*border: 2px solid var\(--ink\)/);
  const block = css.slice(css.indexOf(".public-button-google {"));
  for (const match of block.matchAll(/font-size: ([0-9.]+)px/g)) assert.ok(Number(match[1]) >= 12, `font-size ${match[1]}px below 12px`);
});
