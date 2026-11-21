import assert from "node:assert/strict";
import test from "node:test";
import { reviewCards } from "./source.mjs";

const source = reviewCards();

test("review cards render the review decision proposal and missing facts", () => {
  assert.match(source, /ProposalFacts/);
  assert.match(source, /item\.proposedFacts/);
  assert.match(source, /item\.missingFacts/);
  assert.match(source, /item\.whyNotAutoConfirm/);
});

test("proposed values are labelled as a proposal, not as recorded data", () => {
  assert.match(source, /proposedRow\.length > 0 && <><p className="review-source">Richmod mengusulkan<\/p><dl className="review-proposal">\{proposedRow\.map/);
  assert.doesNotMatch(source, /decisionSource !== "DETERMINISTIC" \? "Richmod mengusulkan" : "Tercatat"/);
});


test("full editing is gated behind an explicit edit step", () => {
  assert.match(source, /useState\(false\)/);
  assert.match(source, /Ubah detail/);
  assert.match(source, /Batal/);
});

test("evidence stays behind a secondary disclosure", () => {
  assert.match(source, /<details className="review-evidence">/);
});

test("a known financial entity is never requested again", () => {
  assert.match(source, /item\.resolvedAccountId/);
  assert.match(source, /item\.resolvedWealthAccountId/);
  assert.doesNotMatch(source, /decision\.resolvedAccountId/);
  assert.match(source, /const needsAccount = !missing \|\|/);
  assert.match(source, /const needsWealth = !missing \|\|/);
});


test("the primary accept action never posts a request the server must reject", () => {
  assert.match(source, /const canQuickAccept =/);
  assert.match(source, /const quickCategoryId = item\.categoryId \|\| proposed\.categoryId \|\| null/);
  assert.match(source, /canQuickAccept \? <button/);
  assert.match(source, /Benar, lanjut isi detail/);
});

test("transaction-date residual opens one native date field and submits it", () => {
  assert.match(source, /transactionAt: missing\.includes\("transaction_at"\)/);
  assert.match(source, /missing\.transactionAt && !dateChoice && <label>Tanggal transaksi<DateInput name="transactionAt" type="date" required\/>/);
  assert.match(source, /transactionAt: dateChoice \? null : form\.get\("transactionAt"\) \|\| null/);
  assert.match(source, /dateChoice: dateChoice \|\| null/);
  assert.match(source, /!missing\.transactionAt/);
});

test("payslip salary choice appears only when API leaves policy unresolved", () => {
  assert.match(source, /const salaryChoice = item.allowedActions?.includes("PRIMARY_SALARY") || item.allowedActions?.includes("ORDINARY_INCOME")/);
  assert.match(source, /{salaryChoice && <label>Catat sebagai<select/);
  assert.match(source, /salaryChoice \? \{ choice: form\.get\("choice"\) \} : \{\}/);
});

test("bank fact completion submits an RFC3339 instant, not a date-only value", () => {
  assert.match(source, /type="datetime-local"/);
  assert.match(source, /transactionAt: raw \? new Date\(raw\)\.toISOString\(\) : null/);
});

test("legacy items fall back to the API missingFields list", () => {
  assert.match(source, /const missing = item\.missingFacts \|\| item\.missingFields/);
});

test("the asset-purchase affordance is reachable without a dead switch", () => {
  assert.match(source, /\{item\.type === "EXPENSE" && \(showAnswers && !editing \? <details className="review-more"><summary>Atau catat sebagai beli aset<\/summary><form onSubmit=\{assetPurchase\}>/);
  assert.match(source, /<\/details> : <form onSubmit=\{assetPurchase\}>/, "the form stays reachable outside the 1-tap layout");
  assert.doesNotMatch(source, /setAsset/);
});

test("merchant input allows learned-category recall without a forced category pick", () => {
  assert.match(source, /const merchantRecallAllowed = missing\.merchant && !categoryRequired/);
  assert.match(source, /required=\{item\.type === "EXPENSE" && !merchantRecallAllowed\}/);
  assert.match(source, /Gunakan kategori merchant tersimpan/);
  assert.match(source, /result\?\.missingFacts\?\.includes\("category"\)/);
  assert.match(source, /setCategoryRequired\(true\)/);
  assert.match(source, /element\.elements\.categoryId\?\.focus\(\)/);
  assert.match(source, /Kategori merchant belum dipelajari\. Pilih kategori\./);
});
