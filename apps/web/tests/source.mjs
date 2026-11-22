import { readFileSync } from "node:fs";

export function globalCss() {
  const entry = new URL("../app/globals.css", import.meta.url);
  return readFileSync(entry, "utf8").replace(/^@import\s+"([^"]+)";\s*$/gm, (_, path) => readFileSync(new URL(path, entry), "utf8"));
}
