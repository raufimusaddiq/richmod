import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

// `npm test` reads sources as text, so a wrong relative import would pass here
// and only fail in `next build`. Resolve every relative import statically.
const root = fileURLToPath(new URL("../app/", import.meta.url));

function walk(dir) {
  return readdirSync(dir).flatMap(name => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : path.endsWith(".js") ? [path] : [];
  });
}

function resolves(from, specifier) {
  const base = join(dirname(from), specifier);
  return [base, `${base}.js`, join(base, "index.js"), `${base}.css`].some(candidate => existsSync(candidate) && statSync(candidate).isFile());
}

test("every relative import in app/ resolves to a file", () => {
  const specifier = /(?:import|export)\s[^"'`]*?from\s*["'](\.{1,2}\/[^"']+)["']|import\s*["'](\.{1,2}\/[^"']+)["']|import\(\s*["'](\.{1,2}\/[^"']+)["']\s*\)/g;
  const broken = [];
  let checked = 0;
  for (const file of walk(root)) {
    const source = readFileSync(file, "utf8");
    for (const match of source.matchAll(specifier)) {
      const target = match[1] || match[2] || match[3];
      checked += 1;
      if (!resolves(file, target)) broken.push(`${file.slice(root.length)} -> ${target}`);
    }
  }
  assert.ok(checked > 60, `expected to inspect the app's imports, saw ${checked}`);
  assert.deepEqual(broken, [], `unresolved relative imports:\n${broken.join("\n")}`);
});

test("the import check would catch the review-card mistake", () => {
  const entry = join(root, "components", "ReviewCards.js");
  assert.equal(resolves(entry, "./CanonicalCard"), false, "cards are not beside ReviewCards.js");
  assert.equal(resolves(entry, "./review/CanonicalCard"), true);
});
