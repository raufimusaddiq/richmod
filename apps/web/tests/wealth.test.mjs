import assert from "node:assert/strict";
import test from "node:test";
import { amountTone } from "../app/lib/format.js";
test("zero money is neutral; only a real amount carries a tone", () => {
  assert.equal(amountTone("0"), "");
  assert.equal(amountTone("0.00"), "");
  assert.equal(amountTone(null), "");
  assert.equal(amountTone("5000000"), "positive");
  assert.equal(amountTone("-5000000"), "negative");
  assert.equal(amountTone("1750000", true), "negative");
  assert.equal(amountTone("0", true), "");
});

