import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const testsDir = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.join(testsDir, "..", "app");
const layout = fs.readFileSync(path.join(appDir, "layout.js"), "utf8");
const interactions = fs.readFileSync(path.join(appDir, "document-card-interactions.css"), "utf8");

test("document card hover uses the neutral navigation surface", () => {
  assert.match(layout, /import "\.\/document-card-interactions\.css";/);
  assert.match(
    interactions,
    /button\.document-card:hover:not\(:disabled\)\s*\{[^}]*background:\s*var\(--surface-muted\)/s,
  );
  assert.doesNotMatch(interactions, /background:\s*var\(--surface-strong\)/);
});
