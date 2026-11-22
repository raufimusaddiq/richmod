import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { buildSite } from "../scripts/build.mjs";
import { routeMeta } from "../app/lib/siteMeta.js";
import { splashDevices } from "../scripts/splash-devices.mjs";

const outDir = mkdtempSync(join(tmpdir(), "richmod-site-"));
const site = await buildSite({ mode: "production", outDir, quiet: true });
const page = name => readFileSync(join(outDir, name), "utf8");
const pageFor = path => page(path === "/" ? "index.html" : `${path.slice(1)}.html`);
const head = html => html.slice(html.indexOf("<head>"), html.indexOf("</head>"));
const rootTitle = "Richmod · Keuangan keluarga, tanpa menebak";
const tag = (html, pattern) => head(html).match(pattern)?.[1];

test.after(() => rmSync(outDir, { recursive: true, force: true }));

test("one hashed script and one hashed stylesheet live under /assets/ and every page references them", () => {
  assert.match(site.script, /^\/assets\/main-[a-z0-9]+\.js$/);
  assert.match(site.stylesheet, /^\/assets\/main-[a-z0-9]+\.css$/);
  assert.deepEqual(readdirSync(join(outDir, "assets")).sort(), [site.script, site.stylesheet].map(path => path.replace("/assets/", "")).sort());
  for (const path of [...Object.keys(routeMeta), "/404"]) {
    const html = path === "/404" ? page("404.html") : pageFor(path);
    assert.ok(html.includes(`href="${site.stylesheet}"`), path);
    assert.ok(html.includes(`<script type="module" src="${site.script}"></script>`), path);
  }
});

test("every route bakes its own title and description, and keeps the root social head", () => {
  for (const [path, meta] of Object.entries(routeMeta)) {
    const html = pageFor(path);
    assert.equal(tag(html, /<title>([^<]*)<\/title>/), meta.title ?? rootTitle, path);
    assert.match(head(html), /<meta name="description" content="[^"]+"\/>/, path);
    assert.equal(tag(html, /<meta property="og:title" content="([^"]*)"/), rootTitle, path);
    assert.equal(tag(html, /<meta name="twitter:card" content="([^"]*)"/), "summary_large_image", path);
    assert.match(tag(html, /<meta property="og:image" content="([^"]*)"/), /^https:\/\/finance\.musaddiqr\.com\/opengraph-image\?[0-9a-f]{16}$/, path);
    assert.equal(tag(html, /<meta name="viewport" content="([^"]*)"/), "width=device-width, initial-scale=1, viewport-fit=cover", path);
    assert.equal(tag(html, /<meta name="theme-color" content="([^"]*)"/), "#245C54", path);
    assert.ok(head(html).includes('<link rel="manifest" href="/manifest.webmanifest"/>'), path);
    assert.ok(head(html).includes('<link rel="icon" href="/icon.svg" type="image/svg+xml"/>'), path);
    assert.ok(head(html).includes('<meta name="apple-mobile-web-app-capable" content="yes"/>'), path);
    assert.equal(head(html).split('rel="apple-touch-startup-image"').length - 1, splashDevices.length, path);
  }
});

test("the 404 page is noindex, titled, and rendered without a route", () => {
  const html = page("404.html");
  assert.ok(head(html).includes('<meta name="robots" content="noindex"/>'));
  assert.equal(tag(html, /<title>([^<]*)<\/title>/), "Halaman tidak ditemukan · Richmod");
  assert.match(html, /<h1>Halaman tidak ditemukan<\/h1>/);
});

test("public pages prerender their content and the app shells prerender a loading state", () => {
  assert.match(pageFor("/privacy"), /<h1>Kebijakan Privasi<\/h1>/);
  assert.match(pageFor("/terms"), /<h1>Ketentuan Layanan<\/h1>/);
  assert.match(pageFor("/invite"), /<h1>Aktifkan akun<\/h1>/);
  for (const path of ["/", "/login", "/transactions", "/documents", "/household", "/inbox", "/settings"]) assert.match(pageFor(path), /role="status"/, path);
  for (const path of ["/analytics", "/wealth", "/admin"]) assert.match(pageFor(path), /Memuat…/, path);
});

test("the legacy redirect pages prerender an empty root", () => {
  for (const path of ["/actions", "/reviews"]) assert.match(pageFor(path), /<div id="root"><\/div>/, path);
});

test("the static files are copied and the opengraph image keeps its public URL as a PNG", () => {
  for (const file of ["manifest.webmanifest", "sw.js", "build-id.txt", "icon.svg", "icons/icon-192.png", "fonts/inter-latin.woff2"]) assert.ok(statSync(join(outDir, file)).isFile(), file);
  const og = readFileSync(join(outDir, "opengraph-image"));
  assert.equal(og.subarray(1, 4).toString(), "PNG");
  assert.equal(og.readUInt32BE(16), 1200);
  assert.equal(og.readUInt32BE(20), 630);
  assert.equal(readFileSync(join(outDir, "build-id.txt"), "utf8"), `${site.buildId}\n`);
});

test("nothing from the old framework ships", () => {
  for (const name of readdirSync(outDir)) assert.ok(!name.startsWith("_next") && !name.startsWith("__next"), name);
  assert.doesNotMatch(readFileSync(join(outDir, site.script.slice(1)), "utf8"), /__next_f|NEXT_REDIRECT/);
});
