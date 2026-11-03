// Stamps the service worker with the release id and the complete chunk list
// derived from the export, and writes it into out/ so webui.go serves it as a
// file. The install handler precaches that list, which is why it ships in the
// worker rather than being fetched from the server after a deploy.
import { readdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { buildId } from "./build-id.mjs";

const root = fileURLToPath(new URL("../out/", import.meta.url));
const template = await readFile(new URL("service-worker.template.js", import.meta.url), "utf8");

async function list(dir, prefix = "") {
  const entries = await readdir(join(root, dir), { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) files.push(...(await list(join(dir, entry.name), path)));
    else files.push(path);
  }
  return files;
}

// Content hashes already name the chunks; files are listed only to precache them.
const files = (await list("_next/static")).sort();
const assets = files.map(path => `/_next/static/${path}`);
const build = process.env.RICHMOD_BUILD_ID || await buildId();
if (!/^[0-9a-f]{12}$/.test(build)) throw new Error(`invalid RICHMOD_BUILD_ID: ${build}`);

const worker = template
  .replaceAll("__BUILD_ID__", build)
  .replace("__ASSETS__", JSON.stringify(assets));
for (const placeholder of ["__BUILD_ID__", "__ASSETS__"]) {
  if (worker.includes(placeholder)) throw new Error(`service worker template still contains ${placeholder}`);
}
await writeFile(join(root, "sw.js"), worker);
console.log(`service worker ${build} over ${assets.length} static files`);
