import { readFileSync, readdirSync } from "node:fs";

// Concatenated source of every .js file directly inside a directory, so a lock
// keeps passing when a large file is split into modules.
export function tree(dir) {
  const base = new URL(`../${dir}/`, import.meta.url);
  return readdirSync(base)
    .filter(name => name.endsWith(".js"))
    .sort()
    .map(name => readFileSync(new URL(name, base), "utf8"))
    .join("\n");
}

// ReviewCards.js plus every card module under components/review.
export function reviewCards() {
  return `${readFileSync(new URL("../app/components/ReviewCards.js", import.meta.url), "utf8")}\n${tree("app/components/review")}`;
}
