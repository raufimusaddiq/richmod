import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";

function reviewTypesFromMigrations() {
  const dir = new URL("../../../db/migrations/", import.meta.url);
  const files = readdirSync(dir).filter(name => name.endsWith(".sql")).sort();
  let types = null;
  for (const name of files) {
    const up = readFileSync(new URL(name, dir), "utf8").split("-- +goose Down")[0];
    const match = up.match(/ADD CONSTRAINT review_item_review_type_check CHECK \(review_type IN \(([^)]*)\)\)/s);
    if (match) types = [...match[1].matchAll(/'([A-Z_]+)'/g)].map(entry => entry[1]);
  }
  return types;
}

const shared = readFileSync(new URL("../app/components/review/shared.js", import.meta.url), "utf8");
const table = shared.slice(shared.indexOf("export const reviewTypeCopy"), shared.indexOf("};", shared.indexOf("export const reviewTypeCopy")));

test("every database review type has an Indonesian label and explanation", () => {
  const types = reviewTypesFromMigrations();
  assert.ok(types && types.length >= 20, "review_item_review_type_check not found");
  for (const type of types) {
    const row = table.match(new RegExp(`\\n  ${type}: \\["([^"]+)", "([^"]+)"\\]`));
    assert.ok(row, `${type} has no label`);
    assert.doesNotMatch(row[1], /_/, `${type} label is a raw code`);
    assert.ok(row[2].length > 20, `${type} needs a one-line explanation`);
  }
});
