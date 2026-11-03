import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Preview server for the static export, used by the Playwright smoke scripts.
// It approximates apps/api/internal/webui and is not the reference behaviour:
// it deliberately does not reproduce the 308 for /path/, the per-class
// Cache-Control headers, or the security headers. Those live in the Go handler
// and are tested there; do not "fix" the Go server to match this preview.
const root = fileURLToPath(new URL("../out/", import.meta.url));
const portFlag = process.argv.indexOf("-p");
const port = Number(process.env.PORT || (portFlag >= 0 ? process.argv[portFlag + 1] : 3000));

const types = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".woff2": "font/woff2",
};

async function file(path) {
  try {
    const info = await stat(join(root, path));
    return info.isFile() ? path : null;
  } catch {
    return null;
  }
}

async function resolve(pathname) {
  if (pathname === "/") return "index.html";
  const path = decodeURIComponent(pathname).replace(/^\/+/, "");
  if (path.includes("..")) return null;
  return (await file(path)) || (await file(`${path}.html`)) || (await file(`${path.replace(/\/$/, "")}/index.html`));
}

createServer(async (request, response) => {
  const { pathname } = new URL(request.url, "http://localhost");
  const path = await resolve(pathname);
  if (!path) {
    const body = await file("404.html");
    response.writeHead(404, { "Content-Type": types[".html"] });
    if (body) createReadStream(join(root, body)).pipe(response);
    else response.end("Not found");
    return;
  }
  response.writeHead(200, { "Content-Type": types[extname(path)] || "application/octet-stream" });
  createReadStream(join(root, path)).pipe(response);
}).listen(port, "127.0.0.1", () => console.log(`static export on http://127.0.0.1:${port}`));
