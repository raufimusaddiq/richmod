import { mkdir, readFile, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { splashDevices } from "./splash-devices.mjs";

const output = fileURLToPath(new URL("../public/splash/", import.meta.url));
const mark = await readFile(new URL("icons/icon-square.svg", import.meta.url), "utf8");
const markSource = `data:image/svg+xml;base64,${Buffer.from(mark).toString("base64")}`;

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
