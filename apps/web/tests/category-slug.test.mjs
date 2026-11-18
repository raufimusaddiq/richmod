// The category slug is derived from the name, so the family never types one.
import assert from "node:assert/strict";
import test from "node:test";
import { categorySlug, uniqueCategorySlug } from "../app/lib/slug.js";

test("category slug is lowercase ASCII, hyphenated, and symbol-free", () => {
  assert.equal(categorySlug("Belanja Rumah"), "belanja-rumah");
  assert.equal(categorySlug("  Makan  "), "makan");
  assert.equal(categorySlug("Café & Resto"), "cafe-resto");
  assert.equal(categorySlug("Kopi—Susu"), "kopi-susu");
  assert.equal(categorySlug("50% Diskon!"), "50-diskon");
  assert.equal(categorySlug(""), "");
  assert.equal(categorySlug("🍜"), "");
});

test("a duplicate name gets a numbered slug under the same parent", () => {
  assert.equal(uniqueCategorySlug("Makan", []), "makan");
  assert.equal(uniqueCategorySlug("Makan", ["makan"]), "makan-2");
  assert.equal(uniqueCategorySlug("Makan", ["makan", "makan-2"]), "makan-3");
  assert.equal(uniqueCategorySlug("🍜", []), "kategori");
});
