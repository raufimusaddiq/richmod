import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join } from "node:path";

const types = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".webmanifest": "application/manifest+json",
  ".woff2": "font/woff2",
};

export function serveDirectory(root, port, label) {
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

  const contentType = path => (path === "opengraph-image" ? types[".png"] : types[extname(path)] || "application/octet-stream");

  return createServer(async (request, response) => {
    const { pathname } = new URL(request.url, "http://localhost");
    const path = await resolve(pathname);
    if (!path) {
      const body = await file("404.html");
      response.writeHead(404, { "Content-Type": types[".html"] });
      if (body) createReadStream(join(root, body)).pipe(response);
      else response.end("Not found");
      return;
    }
    response.writeHead(200, { "Content-Type": contentType(path) });
    createReadStream(join(root, path)).pipe(response);
  }).listen(port, "127.0.0.1", () => console.log(`${label} on http://127.0.0.1:${port}`));
}
