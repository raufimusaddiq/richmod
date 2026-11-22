import { watch } from "node:fs";
import { fileURLToPath } from "node:url";
import { serveDirectory } from "./lib/static-handler.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const port = Number(process.env.PORT || 3000);

let building = false;
let queued = false;

async function rebuild() {
  if (building) {
    queued = true;
    return;
  }
  building = true;
  const started = Date.now();
  const child = Bun.spawn(["bun", "scripts/build.mjs", "--dev"], { cwd: root, stdout: "inherit", stderr: "inherit" });
  const code = await child.exited;
  console.log(code === 0 ? `rebuilt in ${Date.now() - started}ms` : `build failed (exit ${code}); fix the error and save again`);
  building = false;
  if (queued) {
    queued = false;
    await rebuild();
  }
}

await rebuild();
serveDirectory(fileURLToPath(new URL("../out/", import.meta.url)), port, "dev server");

let timer = null;
for (const dir of ["app", "public", "scripts"]) {
  watch(fileURLToPath(new URL(`../${dir}/`, import.meta.url)), { recursive: true }, (_event, name) => {
    if (name && /buildId\.generated|build-id\.txt/.test(name)) return;
    clearTimeout(timer);
    timer = setTimeout(rebuild, 150);
  });
}
