import { createHash } from "node:crypto";
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { basename, extname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { writeBuildId } from "./build-id.mjs";
import { renderDocument } from "./lib/document.mjs";
import { loadPrerenderer } from "./lib/prerender.mjs";
import { metaFor, notFoundMeta, routeMeta } from "../app/lib/siteMeta.js";

const root = fileURLToPath(new URL("../", import.meta.url));
const ASSET_DIR = "assets";

export async function buildSite({ mode = "production", outDir = join(root, "out"), quiet = false } = {}) {
  const log = quiet ? () => {} : message => console.log(message);
  const buildId = writeBuildId(root);
  log(`build id ${buildId}`);

  await rm(outDir, { recursive: true, force: true });
  await mkdir(outDir, { recursive: true });

  const production = mode === "production";
  const bundle = await Bun.build({
    entrypoints: [join(root, "app/main.js")],
    outdir: join(outDir, ASSET_DIR),
    root,
    target: "browser",
    format: "esm",
    splitting: false,
    minify: production,
    sourcemap: production ? "none" : "linked",
    publicPath: `/${ASSET_DIR}/`,
    naming: "[name]-[hash].[ext]",
    external: ["/fonts/*", "/brand/*", "/icons/*"],
    define: { "process.env.NODE_ENV": JSON.stringify(mode) },
  });
  if (!bundle.success) {
    for (const entry of bundle.logs) console.error(entry);
    throw new Error("bundle failed");
  }
  const script = bundle.outputs.find(output => extname(output.path) === ".js");
  const stylesheet = bundle.outputs.find(output => extname(output.path) === ".css");
  if (!script || !stylesheet) throw new Error("bundle is missing its script or stylesheet");

  await cp(join(root, "public"), outDir, { recursive: true });
  const ogImage = await readFile(join(root, "scripts/og/opengraph-image.png"));
  await writeFile(join(outDir, "opengraph-image"), ogImage);
  const ogImageVersion = createHash("sha256").update(ogImage).digest("hex").slice(0, 16);

  const render = await loadPrerenderer();
  const shared = { script: `/${ASSET_DIR}/${basename(script.path)}`, stylesheet: `/${ASSET_DIR}/${basename(stylesheet.path)}`, ogImageVersion };
  const pages = [
    ...Object.keys(routeMeta).map(pathname => ({ pathname, file: pathname === "/" ? "index.html" : `${pathname.slice(1)}.html` })),
    { pathname: "/404", file: "404.html", noindex: true },
  ];
  for (const page of pages) {
    const meta = metaFor(page.pathname) ?? notFoundMeta;
    const html = renderDocument({ ...shared, title: meta.title, description: meta.description, noindex: page.noindex, body: render(page.pathname) });
    await writeFile(join(outDir, page.file), html);
  }
  log(`built ${pages.length} pages, ${basename(script.path)} (${Math.round(script.size / 1024)} kB), ${basename(stylesheet.path)} (${Math.round(stylesheet.size / 1024)} kB)`);
  return { outDir, buildId, script: shared.script, stylesheet: shared.stylesheet };
}

if (import.meta.main) {
  await buildSite({ mode: process.argv.includes("--dev") ? "development" : "production" });
}
