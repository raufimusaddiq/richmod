// Settings interaction/a11y locks: deactivation asks first, a change says it worked,
// a failure is announced where it happened, and repeated row actions are named.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const settings = read("app/settings/page.js");
const household = read("app/household/page.js");

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
  assert.match(settings, /if \(id\) setHighlight\(id\)/);
  assert.match(settings, /className=\{highlight === item\.id \? "is-new" : undefined\}/);
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
  assert.match(settings, /setError\(\{ key: errorKey, field, message:/);
  assert.match(settings, /<RowError k="account-new" error=\{error\}\/>/);
  assert.match(settings, /<RowError k=\{item\.id\} error=\{error\}\/>/);
  // An empty message renders nothing, so an alert never appears blank.
  assert.match(settings, /if \(!error\?\.message \|\| error\.key !== k\) return null;/);
  assert.match(settings, /<p className="field-error" id=\{`\$\{k\}-error`\} role="alert">\{error\.message\}<\/p>/);
  // The top notice is for page-load failures only.
  assert.match(settings, /<ErrorNotice message=\{loadNotice \|\| loader\.error\} retry=\{\(\) => load\(\)\}\/>/);
  assert.doesNotMatch(settings, /ErrorNotice message=\{error/);
  // A rejected field is marked and points at its message.
  assert.match(settings, /"aria-invalid": "true", "aria-describedby": `\$\{key\}-error`/);
  assert.match(settings, /fieldError\("listener-new", "senderAddress"\)/);
  // The capability checkboxes are the field the server rejects, so they carry the mark.
  assert.match(settings, /fieldError\("financial-source-new", "cashMovement"\)/);
  assert.match(settings, /fieldError\(`edit-\$\{editingFinancialSource\.id\}`, "cashMovement"\)/);
});

test("every error key has a renderer, and an unmatched key still reaches the page", () => {
  const keys = new Set();
  for (const match of settings.matchAll(/request\("([a-z-]+)"/g)) keys.add(match[1]);
  for (const match of settings.matchAll(/setError\(\{ key: "([a-z-]+)"/g)) keys.add(match[1]);
  for (const match of settings.matchAll(/errorKey: "([a-z-]+)"/g)) keys.add(match[1]);
  assert.ok(keys.size >= 8, `expected the fixed keys, found ${[...keys].join(", ")}`);
  for (const key of keys) assert.match(settings, new RegExp(`<RowError k="${key}"`), `no RowError renders ${key}`);
  // The declared set is exactly the rendered set, and anything else falls back to the top.
  const declared = settings.match(/SECTION_ERROR_KEYS = new Set\(\[([^\]]+)\]\)/)[1].match(/"([a-z-]+)"/g).map(value => value.slice(1, -1));
  assert.deepEqual([...keys].sort(), declared.sort());
  assert.match(settings, /const unrendered = error && !SECTION_ERROR_KEYS\.has\(error\.key\)/);
  assert.match(settings, /<ErrorNotice message=\{unrendered\}\/>/);
});

test("household add, invite and deactivate report success and return focus", () => {
  assert.match(household, /<Toast message=\{toast\} onClose=\{closeToast\}\/>/);
  assert.match(household, /setToast\(`Anggota \$\{displayName\} ditambahkan\.`\)/);
  assert.match(household, /setToast\(`Anggota \$\{members\.find\(item => item\.id === memberId\)\?\.displayName \|\| "anggota"\} dinonaktifkan\.`\)/);
  assert.match(household, /setToast\(kind === "telegram" \? `Undangan Telegram untuk \$\{name\} dibuat\.`/);
  assert.match(household, /className=\{highlight === member\.id \? "is-new" : undefined\}/);
  // Focus: the form's first field after an add, the row after a deactivate, the link after an invite.
  assert.match(household, /formEl\.reset\(\); formEl\.querySelector\("input"\)\?\.focus\(\);/);
  assert.match(household, /document\.getElementById\(`member-\$\{memberId\}`\)\?\.focus\(\)/);
  assert.match(household, /if \(invite\?\.link\) linkInput\.current\?\.focus\(\)/);
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

test("settings explains its jargon and derives the category slug", () => {
  // The slug is no longer a field the family has to fill in.
  assert.doesNotMatch(settings, /Slug kategori/);
  assert.match(settings, /uniqueCategorySlug\(name,/);
  // It is derived and never shown: no helper line, no slug under a category.
  assert.doesNotMatch(settings, /Slug dibuat otomatis/);
  assert.doesNotMatch(settings, /\{item\.slug\}/);
  // Internal names are gone from the page.
  for (const term of ["gateway", "Petunjuk tersamar", "Kebijakan pencatatan", "Tambah draf"]) {
    assert.doesNotMatch(settings, new RegExp(term), term);
  }
  // The provider edit action is Indonesian and named like the other row actions.
  assert.doesNotMatch(settings, />Edit</);
  assert.match(settings, /aria-label=\{`Ubah \$\{item\.providerName\}`\}[\s\S]*?>Ubah</);
  // Every helper line is linked from the field it explains.
  for (const id of ["account-tracking-hint", "wealth-role-hint", "wealth-source-hint", "known-hint", "provider-wealth-hint", "provider-edit-wealth-hint"]) {
    assert.match(settings, new RegExp(`aria-describedby="${id}"`), id);
    assert.match(settings, new RegExp(`<FieldHint id="${id}">`), id);
  }
});

test("a helper line under one control never lifts its neighbours", () => {
  const css = globalCss();
  // The row aligns at the top, so a taller label (with a hint) does not move its siblings.
  assert.match(css, /\.inline-form \{ align-items: start; \}/);
  // The submit button is offset past the label line to sit on the control row.
  assert.match(css, /\.inline-form > button \{ align-self: start; margin-top: 24px; \}/);
  // On a phone the button takes its own full-width row, so the offset is dropped.
  assert.match(css, /\.inline-form > button, \.member-form > button \{ flex: 1 1 100%; margin-top: 0; \}/);
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
