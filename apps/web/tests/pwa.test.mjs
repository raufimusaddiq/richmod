import assert from "node:assert/strict";
import { existsSync, readFileSync, statSync } from "node:fs";
import { inflateSync } from "node:zlib";
import test from "node:test";

const file = path => new URL(`../${path}`, import.meta.url);
const manifest = JSON.parse(readFileSync(file("public/manifest.webmanifest"), "utf8"));

// Minimal PNG reader for the committed icons: 8-bit RGB/RGBA, non-interlaced,
// which is what scripts/build-icons.mjs emits. Decoding pixels lets the safe
// zone be checked instead of trusted.
function decodePng(buffer) {
  assert.deepEqual([...buffer.subarray(0, 8)], [137, 80, 78, 71, 13, 10, 26, 10], "PNG signature");
  let offset = 8;
  let width = 0;
  let height = 0;
  let channels = 0;
  const chunks = [];
  while (offset < buffer.length) {
    const length = buffer.readUInt32BE(offset);
    const type = buffer.toString("ascii", offset + 4, offset + 8);
    const data = buffer.subarray(offset + 8, offset + 8 + length);
    if (type === "IHDR") {
      width = data.readUInt32BE(0);
      height = data.readUInt32BE(4);
      assert.equal(data[8], 8, "icon bit depth");
      assert.ok(data[9] === 2 || data[9] === 6, "icon colour type is RGB or RGBA");
      assert.equal(data[12], 0, "icon is not interlaced");
      channels = data[9] === 6 ? 4 : 3;
    } else if (type === "IDAT") chunks.push(data);
    else if (type === "IEND") break;
    offset += 12 + length;
  }
  const raw = inflateSync(Buffer.concat(chunks));
  const stride = width * channels;
  const pixels = Buffer.alloc(height * stride);
  for (let y = 0; y < height; y += 1) {
    const filter = raw[y * (stride + 1)];
    const line = raw.subarray(y * (stride + 1) + 1, (y + 1) * (stride + 1));
    for (let x = 0; x < stride; x += 1) {
      const left = x >= channels ? pixels[y * stride + x - channels] : 0;
      const above = y > 0 ? pixels[(y - 1) * stride + x] : 0;
      const corner = y > 0 && x >= channels ? pixels[(y - 1) * stride + x - channels] : 0;
      let value = line[x];
      if (filter === 1) value += left;
      else if (filter === 2) value += above;
      else if (filter === 3) value += Math.floor((left + above) / 2);
      else if (filter === 4) {
        const estimate = left + above - corner;
        const distances = [Math.abs(estimate - left), Math.abs(estimate - above), Math.abs(estimate - corner)];
        value += distances[0] <= distances[1] && distances[0] <= distances[2] ? left : distances[1] <= distances[2] ? above : corner;
      }
      pixels[y * stride + x] = value & 0xff;
    }
  }
  return { width, height, channels, pixels };
}

test("manifest is installable on iOS Safari and Android Chrome", () => {
  assert.equal(manifest.name, "Richmod — Keuangan keluarga");
  assert.equal(manifest.short_name, "Richmod");
  assert.equal(manifest.start_url, "/");
  assert.equal(manifest.scope, "/");
  assert.equal(manifest.display, "standalone");
  assert.match(manifest.background_color, /^#[0-9a-f]{6}$/i);
  assert.match(manifest.theme_color, /^#[0-9a-f]{6}$/i);
  assert.equal(manifest.id, "/");
});

test("manifest icons are real PNG files at the required sizes", () => {
  const required = [["192x192", "any"], ["512x512", "any"], ["512x512", "maskable"]];
  for (const [sizes, purpose] of required) {
    const icon = manifest.icons.find(entry => entry.sizes === sizes && entry.purpose === purpose);
    assert.ok(icon, `${sizes} ${purpose} icon is listed`);
    assert.equal(icon.type, "image/png");
    const path = file(`public${icon.src}`);
    assert.ok(statSync(path).isFile(), `${icon.src} is committed`);
    const png = decodePng(readFileSync(path));
    const [width, height] = sizes.split("x").map(Number);
    assert.deepEqual([png.width, png.height], [width, height], `${icon.src} pixel size`);
  }
});

test("apple-touch-icon is a real opaque 180x180 PNG", () => {
  const png = decodePng(readFileSync(file("public/icons/apple-touch-icon-180.png")));
  assert.deepEqual([png.width, png.height], [180, 180]);
  assert.equal(png.channels, 3, "iOS renders transparency as black");
});

test("maskable icon keeps its mark inside the 80% safe circle", () => {
  const png = decodePng(readFileSync(file("public/icons/maskable-512.png")));
  const centre = png.width / 2;
  const radius = png.width * 0.4;
  let mark = 0;
  for (let y = 0; y < png.height; y += 1) {
    for (let x = 0; x < png.width; x += 1) {
      const at = (y * png.width + x) * png.channels;
      if (png.pixels[at] > 180 && png.pixels[at + 1] > 180) {
        mark += 1;
        assert.ok(Math.hypot(x + 0.5 - centre, y + 0.5 - centre) <= radius, `mark pixel outside safe circle at ${x},${y}`);
      }
    }
  }
  assert.ok(mark > png.width * png.height * 0.1, "maskable icon contains the mark");
  const corner = png.pixels[0];
  assert.ok(corner < 100, "maskable icon is full-bleed, not transparent at the corners");
});

test("built export ships a stamped worker and the manifest", { skip: !existsSync(file("out/sw.js")) }, () => {
  const worker = readFileSync(file("out/sw.js"), "utf8");
  assert.doesNotMatch(worker, /__BUILD_ID__/, "build stamp was substituted");
  assert.match(worker, /const BUILD = "[0-9a-f]{12}";/);
  assert.equal(readFileSync(file("out/manifest.webmanifest"), "utf8"), readFileSync(file("public/manifest.webmanifest"), "utf8"));
  for (const icon of manifest.icons) assert.ok(existsSync(file(`out${icon.src}`)), `${icon.src} is exported`);
});
