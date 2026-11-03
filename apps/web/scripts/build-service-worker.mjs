// Stamps the service worker with a release identity derived from the export it
// will control, and writes it into out/ so webui.go can serve it as a file.
import { createHash } from "node:crypto";
import { readdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../out/", import.meta.url));
const template = await readFile(new URL("service-worker.template.js", import.meta.url), "utf8");

async function list(dir, prefix = "") {
  const entries = await readdir(join(root, dir), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) files.push(...(await list(join(dir, entry.name), path)));
    else files.push(`${path}:${(await readFile(join(root, dir, entry.name))).length}`);
  }
  return files;
}

// Content hashes already name the chunks; their sorted list is the build
// identity. It changes whenever any page, chunk or public file changes.
const files = await list("_next/static");
files.sort();
const build = createHash("sha256").update(files.join("\n")).digest("hex").slice(0, 12);

const worker = template.replaceAll("__BUILD_ID__", build);
if (worker.includes("__BUILD_ID__")) throw new Error("service worker template still contains __BUILD_ID__");
await writeFile(join(root, "sw.js"), worker);
console.log(`service worker ${build} over ${files.length} static files`);
