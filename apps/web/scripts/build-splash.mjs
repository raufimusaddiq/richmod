// Rasterises the iOS launch images into public/splash. Run with
// `node apps/web/scripts/build-splash.mjs` after editing the device table or
// the brand mark. Chromium is already a dev dependency (build-icons.mjs uses
// it), so this adds nothing to the runtime: webui.go serves the committed PNGs
// as ordinary public files, and `next build` copies them into out/.
import { mkdir, readFile, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { splashDevices } from "./splash-devices.mjs";

const output = fileURLToPath(new URL("../public/splash/", import.meta.url));
// The same art as the home-screen icon, so the launch image and the icon the
// user tapped are the same mark.
const mark = await readFile(new URL("icons/icon-square.svg", import.meta.url), "utf8");
const markSource = `data:image/svg+xml;base64,${Buffer.from(mark).toString("base64")}`;

// Full-bleed manifest background colour, the mark centred at 20% of the width,
// nothing else. 22.37% is the corner radius iOS gives a home-screen icon, so
// the mark keeps the shape the user tapped.
const page = width => {
  const size = Math.round(width * 0.2);
  return `<!doctype html><html><head><style>
html, body { margin: 0; width: 100%; height: 100%; background: #f7f3e8; }
body { display: grid; place-items: center; }
img { display: block; width: ${size}px; height: ${size}px; border-radius: 22.37%; }
</style></head><body><img src="${markSource}" alt=""></body></html>`;
};

await mkdir(output, { recursive: true });
const browser = await chromium.launch();
let total = 0;
try {
  for (const device of splashDevices) {
    const tab = await browser.newPage({ viewport: { width: device.pixelWidth, height: device.pixelHeight }, deviceScaleFactor: 1 });
    await tab.setContent(page(device.pixelWidth), { waitUntil: "load" });
    await tab.screenshot({ path: output + device.file, animations: "disabled" });
    await tab.close();
    const bytes = (await stat(output + device.file)).size;
    total += bytes;
    console.log(`${device.file} ${device.pixelWidth}x${device.pixelHeight} ${bytes} B ${device.media}`);
  }
} finally {
  await browser.close();
}
console.log(`${splashDevices.length} files, ${(total / 1024).toFixed(0)} KiB total`);
