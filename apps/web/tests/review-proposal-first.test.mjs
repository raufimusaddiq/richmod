import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

// PRD §13.1/§13.4: the Inbox is proposal-first, and it may render a required
// input only for a fact the stored ReviewDecision named as missing. These are
// source-level guards, because the card components are JSX without a test renderer.
const source = await readFile(new URL("../app/components/ReviewCards.js", import.meta.url), "utf8");

test("review cards render the review decision proposal and missing facts", () => {
  assert.match(source, /ProposalFacts/);
  assert.match(source, /decision\?\.proposedFacts/);
  assert.match(source, /decision\?\.missingFacts/);
  assert.match(source, /decision\?\.whyNotAutoConfirm/);
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

