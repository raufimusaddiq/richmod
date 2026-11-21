import { readFileSync, readdirSync } from "node:fs";

export function tree(dir) {
  const base = new URL(`../${dir}/`, import.meta.url);
  return readdirSync(base)
    .filter(name => name.endsWith(".js"))
    .sort()
    .map(name => readFileSync(new URL(name, base), "utf8"))
    .join("\n");
}

export function reviewCards() {
  return `${readFileSync(new URL("../app/components/ReviewCards.js", import.meta.url), "utf8")}\n${tree("app/components/review")}`;
}

export function globalCss() {
  const entry = new URL("../app/globals.css", import.meta.url);
  return readFileSync(entry, "utf8").replace(/^@import\s+"([^"]+)";\s*$/gm, (_, path) => readFileSync(new URL(path, entry), "utf8"));
}
