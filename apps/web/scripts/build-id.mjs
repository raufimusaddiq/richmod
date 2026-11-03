// Release identity for the web export, derived from the frontend sources before
// Next runs. The same value is inlined into the page (meta tag) and into the
// service worker (cache generation), so a window can name its build exactly
// instead of inferring it from script URLs that several builds share.
import { createHash } from "node:crypto";
import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const inputs = ["app", "public", "scripts", "next.config.mjs", "package.json", "package-lock.json"];
const skip = new Set(["node_modules", "out", ".next", "test-results"]);

async function list(dir, prefix = "") {
  const entries = await readdir(join(root, dir), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (skip.has(entry.name)) continue;
    const path = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) files.push(...(await list(join(dir, entry.name), path)));
    else files.push(path);
  }
  return files;
}

export async function buildId() {
  const files = [];
  for (const input of inputs) {
    if (input.includes(".")) files.push({ path: input, full: join(root, input) });
    else for (const path of await list(input, input)) files.push({ path, full: join(root, path) });
  }
  files.sort((left, right) => (left.path < right.path ? -1 : 1));
  const hash = createHash("sha256");
  for (const file of files) {
    hash.update(file.path);
    hash.update(await readFile(file.full));
  }
  return hash.digest("hex").slice(0, 12);
}
