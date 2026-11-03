// Rasterises the PWA icon sources into the real PNG files committed under
// public/icons. Run with `node scripts/build-icons.mjs` after editing an SVG.
// Chromium is already a dev dependency for the Playwright smokes, so this adds
// no new dependency and no runtime route: webui.go serves the PNGs as files.
import { mkdir } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const source = name => pathToFileURL(fileURLToPath(new URL(`icons/${name}.svg`, import.meta.url))).href;
const output = fileURLToPath(new URL("../public/icons/", import.meta.url));
const targets = [
  ["icon-square", "icon-192.png", 192],
  ["icon-square", "icon-512.png", 512],
  ["icon-square", "apple-touch-icon-180.png", 180],
  ["icon-maskable", "maskable-512.png", 512],
];

await mkdir(output, { recursive: true });
const browser = await chromium.launch();
try {
  for (const [svg, file, size] of targets) {
    const page = await browser.newPage({ viewport: { width: size, height: size }, deviceScaleFactor: 1 });
    await page.goto(source(svg));
    await page.screenshot({ path: output + file, omitBackground: false });
    await page.close();
  }
} finally {
  await browser.close();
}
console.log(targets.map(([, file, size]) => `${file} ${size}x${size}`).join("\n"));
