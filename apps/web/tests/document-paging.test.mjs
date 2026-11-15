import assert from "node:assert/strict";
import test from "node:test";
import { mergeFreshDocuments } from "../app/lib/documentPaging.js";

test("a row tied with the page boundary timestamp survives a refresh", () => {
  const at = "2026-11-15T10:00:00Z";
  const fresh = [{ id: "c", createdAt: "2026-11-15T11:00:00Z" }, { id: "b", createdAt: at }];
  const current = [{ id: "b", createdAt: at }, { id: "a", createdAt: at }, { id: "z", createdAt: "2026-11-14T10:00:00Z" }];
  assert.deepEqual(mergeFreshDocuments(fresh, current).map(item => item.id), ["c", "b", "a", "z"]);
});

test("a refresh without older pages returns the fresh page and does not duplicate", () => {
  const fresh = [{ id: "a" }, { id: "b" }];
  assert.deepEqual(mergeFreshDocuments(fresh, fresh), fresh);
});
