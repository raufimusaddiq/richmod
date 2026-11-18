// Settings interaction/a11y locks: deactivation asks first, a change says it worked,
// a failure is announced where it happened, and repeated row actions are named.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const settings = read("app/settings/page.js");
const household = read("app/household/page.js");

// Every <button> whose visible text is one of the repeated row actions.
const rowButtons = (source, texts) => [...source.matchAll(/<button\b[^>]*>([^<]*)<\/button>/g)]
  .filter(match => texts.includes(match[1].trim()))
  .map(match => match[0]);

test("every deactivate asks first and names the consequence", () => {
  // No row action may still patch straight through: deactivation goes through toggle().
  assert.doesNotMatch(settings, /onClick=\{\(\) => patch\(item\.id,/, "a toggle bypasses the confirm dialog");
  assert.match(settings, /if \(active && !\(await confirm\(message, \{ confirmLabel: "Nonaktifkan", danger: true \}\)\)\) return false;/);
  const toggles = [...settings.matchAll(/toggle\(item\.id,/g)];
  assert.ok(toggles.length >= 7, `expected every list to confirm, found ${toggles.length}`);
  const consequences = [...settings.matchAll(/message: `Nonaktifkan [^`]+`/g)];
  assert.ok(consequences.length >= 6, "each deactivate states its consequence in plain words");
  // Activating is not consequential, so it never asks.
  assert.match(settings, /onClick=\{event => toggle\(item\.id, `financial-email-sources\/\$\{item\.id\}`, \{ status: "ACTIVE" \}, \{ success:/);
});

test("a completed change shows a specific toast and flashes the row", () => {
  assert.match(settings, /<Toast message=\{toast\} onClose=\{closeToast\}\/>/);
  assert.match(settings, /if \(success\) setToast\(success\)/);
  assert.match(settings, /if \(name\) setHighlight\(name\)/);
  assert.match(settings, /className=\{highlight === item\.name \? "is-new" : undefined\}/);
  assert.match(globalCss(), /\.settings-list article\.is-new \{ animation: row-highlight/);
  const messages = [...settings.matchAll(/success: `[^`]+`/g)];
  assert.ok(messages.length >= 8, `expected a toast per change, found ${messages.length}`);
  // Focus returns to the form's first field after an add.
  assert.match(settings, /formEl\.reset\(\); formEl\.querySelector\("input"\)\?\.focus\(\);/);
  // ...and to the row action after a toggle.
  assert.match(settings, /if \(done && button\?\.isConnected\) button\.focus\(\);/);
  // Every toast names the item it changed, including the email ingress.
  assert.match(settings, /setToast\(`Email \$\{data\.emailIngress\?\.address \|\| "bank"\} diaktifkan\.`\)/);
});

test("the toast clears the mobile tab bar", () => {
  const css = globalCss();
  // 66px tab bar + 12px gap, plus the safe area; the toast stays inside the viewport.
  const mobile = css.slice(css.indexOf("@media (max-width: 1100px)"));
  assert.match(mobile, /\.toast \{[^}]*left: 16px;[^}]*right: 16px;[^}]*bottom: calc\(78px \+ env\(safe-area-inset-bottom\)\);[^}]*max-width: none;/);
  // Desktop keeps its 24px corner.
  assert.match(css, /\.toast \{ position: fixed; right: 24px; bottom: 24px;/);
});

test("a failed change is announced in its own form or row, not at the page top", () => {
  assert.match(settings, /setError\(\{ key, field, message:/);
  assert.match(settings, /<RowError k="account-new" error=\{error\}\/>/);
  assert.match(settings, /<RowError k=\{item\.id\} error=\{error\}\/>/);
  assert.match(settings, /function RowError\(\{ k, error \}\) \{\s*return error\?\.key === k \? <p className="field-error" id=\{`\$\{k\}-error`\} role="alert">/);
  // The top notice is for page-load failures only.
  assert.match(settings, /<ErrorNotice message=\{loadNotice \|\| loader\.error\} retry=\{\(\) => load\(\)\}\/>/);
  assert.doesNotMatch(settings, /ErrorNotice message=\{error/);
  // A rejected field is marked and points at its message.
  assert.match(settings, /"aria-invalid": "true", "aria-describedby": `\$\{key\}-error`/);
  assert.match(settings, /fieldError\("listener-new", "senderAddress"\)/);
});

test("required fields carry one visible marker across settings and household", () => {
  assert.match(globalCss(), /\.req \{ color: var\(--danger\); \}/);
  assert.match(settings, /<p className="form-legend"><span className="req" aria-hidden="true">\*<\/span> wajib diisi<\/p>/);
  assert.match(household, /<p className="form-legend"><span className="req" aria-hidden="true">\*<\/span> wajib diisi<\/p>/);
  for (const label of ["Nama rekening", "Nama akun", "Nama bank", "Nama kategori", "Nama provider"]) {
    assert.match(settings, new RegExp(`<span>${label} <span className="req" aria-hidden="true">\\*</span></span>`), label);
  }
  assert.match(household, /<span>Nama anggota <span className="req" aria-hidden="true">\*<\/span><\/span>/);
  assert.match(household, /<span>Email anggota <span className="req" aria-hidden="true">\*<\/span><\/span>/);
});

// Every <button> element, from its opening tag to its closing tag.
const buttons = source => source.split("<button").slice(1).map(chunk => chunk.split("</button>")[0]);

test("repeated row actions carry the item in their accessible name", () => {
  const actionTags = buttons(settings)
    .filter(tag => /toggle\(item\.id,|rename\(item,|testFinancialSource\(item\)|editFinancialSource\(item\)/.test(tag));
  assert.ok(actionTags.length >= 12, `expected every row action, found ${actionTags.length}`);
  for (const tag of actionTags) assert.match(tag, /aria-label=\{`/, tag);
  assert.match(settings, /aria-label=\{`\$\{item\.active \? "Nonaktifkan" : "Aktifkan"\} \$\{item\.bankName\}`\}/);
  assert.match(settings, /aria-label=\{`Ubah nama \$\{item\.name\}`\}/);
  assert.match(settings, /aria-label=\{`Uji \$\{item\.providerName\}`\}/);
  // Household rows are named the same way.
  assert.match(household, /aria-label=\{`Nonaktifkan \$\{member\.displayName\}`\}/);
  const householdActions = buttons(household)
    .filter(tag => /member\.id/.test(tag) && /deactivate\(member\.id\)|revokeInvite\(|renewInvite\(|createInvite\(|createDashboardInvite\(/.test(tag));
  assert.ok(householdActions.length >= 6, `expected every household row action, found ${householdActions.length}`);
  for (const tag of householdActions) assert.match(tag, /aria-label=\{`/, tag);
});
