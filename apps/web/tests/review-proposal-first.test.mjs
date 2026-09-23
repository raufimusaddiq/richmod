import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

// PRD §13.1/§13.4: the Inbox is proposal-first, and it may render a required
// input only for a fact the stored ReviewDecision named as missing. These are
// source-level guards, because the card components are JSX without a test renderer.
const source = await readFile(new URL("../app/components/ReviewCards.js", import.meta.url), "utf8");

test("review cards render the review decision proposal and missing facts", () => {
  assert.match(source, /ProposalFacts/);
  assert.match(source, /item\.proposedFacts/);
  assert.match(source, /item\.missingFacts/);
  assert.match(source, /item\.whyNotAutoConfirm/);
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
  assert.match(source, /resolvedAccountId/);
  assert.match(source, /resolvedWealthAccountId/);
  assert.match(source, /const needsAccount = !missing \|\|/);
  assert.match(source, /const needsWealth = !missing \|\|/);
});


test("the primary accept action sends the proposed category, not a null one", () => {
  // The AMBIGUOUS_CATEGORY case is exactly the one where item.categoryId is null,
  // so a quick accept that only sends item.categoryId returns 400 (Hermes review).
  assert.match(source, /categoryId: item\.categoryId \|\| proposed\.categoryId \|\| null/);
});

test("the asset-purchase affordance is reachable without a dead switch", () => {
  // A useState whose setter has no call site silently removed the asset-purchase
  // form from EXPENSE reviews; it must render on the item type alone.
  assert.match(source, /\{item\.type === "EXPENSE" && <form onSubmit=\{assetPurchase\}>/);
  assert.doesNotMatch(source, /setAsset/);
});
