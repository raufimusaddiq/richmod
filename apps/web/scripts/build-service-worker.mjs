// Stamps the service worker with a release identity and the complete chunk list
// derived from the export, and writes it into out/ so webui.go serves it as a
// file. The install handler precaches that list, which is why it ships in the
// worker rather than being fetched from the server after a deploy.
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
    else files.push({ path, size: (await readFile(join(root, dir, entry.name))).length });
  }
  return files;
}

// Content hashes already name the chunks; their sorted list is the build
// identity. It changes whenever any page, chunk or public file changes.
const files = (await list("_next/static")).sort((left, right) => (left.path < right.path ? -1 : 1));
const build = createHash("sha256").update(files.map(file => `${file.path}:${file.size}`).join("\n")).digest("hex").slice(0, 12);
const assets = files.map(file => `/_next/static/${file.path}`);

const worker = template
  .replaceAll("__BUILD_ID__", build)
  .replace("__ASSETS__", JSON.stringify(assets));
for (const placeholder of ["__BUILD_ID__", "__ASSETS__"]) {
  if (worker.includes(placeholder)) throw new Error(`service worker template still contains ${placeholder}`);
}
await writeFile(join(root, "sw.js"), worker);
console.log(`service worker ${build} over ${assets.length} static files`);
