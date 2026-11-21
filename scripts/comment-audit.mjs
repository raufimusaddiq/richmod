#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { collect } from "./check-comments.mjs";

const CATEGORIES = new Set(["T", "S", "E", "D", "K"]);
const ADR_FILE = "docs/adr/README.md";

function repoRoot() {
  return execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function writeJson(path, value) {
  writeFileSync(path, JSON.stringify(value));
}

function buildBlocks(files) {
  const flat = [];
  for (const [path, comments] of files) for (const c of comments) flat.push({ path, ...c });
  const blocks = [];
  let cur = null;
  for (const c of flat) {
    const joins =
      cur &&
      cur.path === c.path &&
      !c.trailing &&
      !cur.trailing &&
      c.kind === "line" &&
      cur.kind === "line" &&
      c.line === cur.endLine + 1;
    if (joins) {
      cur.endLine = c.endLine;
      cur.text += "\n" + c.text;
      continue;
    }
    cur = {
      id: blocks.length,
      path: c.path,
      lang: c.lang,
      line: c.line,
      endLine: c.endLine,
      trailing: c.trailing,
      kind: c.kind,
      text: c.text,
    };
    blocks.push(cur);
  }
  return blocks;
}

function renderBlock(root, cache, b) {
  if (!cache.has(b.path)) cache.set(b.path, readFileSync(resolve(root, b.path), "utf8").split(/\r?\n/));
  const lines = cache.get(b.path);
  const code = b.trailing ? [lines[b.line - 1]] : lines.slice(b.endLine, b.endLine + 4);
  const range = b.endLine > b.line ? `-${b.endLine}` : "";
  return `### id=${b.id} ${b.path}:${b.line}${range}${b.trailing ? " [trailing]" : ""}\n${b.text}\n--- code ${
    b.trailing ? "(same line)" : "after"
  }\n${code.map((l) => l.slice(0, 200)).join("\n")}\n`;
}

function cmdChunk(dir, chunkCount) {
  const root = repoRoot();
  mkdirSync(join(dir, "chunks"), { recursive: true });
  mkdirSync(join(dir, "cls"), { recursive: true });
  const blocks = buildBlocks(collect(root));
  writeJson(join(dir, "blocks.json"), blocks);

  const byFile = new Map();
  for (const b of blocks) {
    if (!byFile.has(b.path)) byFile.set(b.path, []);
    byFile.get(b.path).push(b);
  }
  const cache = new Map();
  const files = [...byFile.keys()].sort();
  const rendered = new Map(files.map((f) => [f, byFile.get(f).map((b) => renderBlock(root, cache, b)).join("\n")]));
  const total = [...rendered.values()].reduce((a, r) => a + r.length, 0);
  const target = total / chunkCount;

  const chunks = [];
  let text = "";
  let ids = [];
  for (const f of files) {
    const r = rendered.get(f);
    if (text && text.length + r.length > target * 1.15 && chunks.length < chunkCount - 1) {
      chunks.push({ text, ids });
      text = "";
      ids = [];
    }
    text += r + "\n";
    ids.push(...byFile.get(f).map((b) => b.id));
  }
  if (text) chunks.push({ text, ids });

  chunks.forEach((c, i) => {
    const n = String(i + 1).padStart(2, "0");
    writeFileSync(join(dir, "chunks", `chunk-${n}.txt`), c.text);
    writeJson(join(dir, "chunks", `chunk-${n}.ids.json`), c.ids);
    process.stdout.write(`chunk-${n}: ${c.ids.length} blocks, ${Math.round(c.text.length / 1000)}k chars\n`);
  });
  process.stdout.write(`${blocks.length} blocks in ${files.length} files\n`);
}

function cmdMerge(dir) {
  const blocks = readJson(join(dir, "blocks.json"));
  const all = new Map();
  let problems = 0;
  const idFiles = readdirSync(join(dir, "chunks")).filter((f) => f.endsWith(".ids.json")).sort();
  for (const idFile of idFiles) {
    const name = idFile.replace(".ids.json", "");
    const ids = readJson(join(dir, "chunks", idFile));
    let entries;
    try {
      entries = readJson(join(dir, "cls", `${name}.json`));
    } catch (e) {
      process.stderr.write(`${name}: unreadable classification (${e.message})\n`);
      problems++;
      continue;
    }
    const seen = new Set();
    for (const e of entries) {
      if (seen.has(e.id)) problems += report(`${name}: duplicate id ${e.id}`);
      seen.add(e.id);
      if (!ids.includes(e.id)) problems += report(`${name}: foreign id ${e.id}`);
      if (!CATEGORIES.has(e.c)) problems += report(`${name}: bad category on ${e.id}`);
      if (e.c === "D" && (!e.topic || !e.decision)) problems += report(`${name}: D ${e.id} lacks topic/decision`);
      if (e.c === "E" && (!e.title || !e.hack || !e.fix)) problems += report(`${name}: E ${e.id} lacks title/hack/fix`);
      all.set(e.id, e);
    }
    for (const id of ids) if (!seen.has(id)) problems += report(`${name}: id ${id} unclassified`);
  }
  writeJson(join(dir, "cls-all.json"), [...all.values()]);
  const totals = {};
  const lines = {};
  for (const b of blocks) {
    const e = all.get(b.id);
    if (!e) continue;
    totals[e.c] = (totals[e.c] ?? 0) + 1;
    lines[e.c] = (lines[e.c] ?? 0) + (b.endLine - b.line + 1);
  }
  process.stdout.write(`classified ${all.size}/${blocks.length}; blocks ${JSON.stringify(totals)}; lines ${JSON.stringify(lines)}\n`);
  if (problems > 0) process.exit(1);
}

function report(message) {
  process.stderr.write(message + "\n");
  return 1;
}

function cmdAdrIn(dir, maxPerCluster) {
  const blocks = readJson(join(dir, "blocks.json"));
  const entries = readJson(join(dir, "cls-all.json"))
    .filter((e) => e.c === "D" && !e.existing_adr)
    .map((e) => ({ e, b: blocks[e.id] }))
    .sort((x, y) => (x.b.path < y.b.path ? -1 : x.b.path > y.b.path ? 1 : x.b.line - y.b.line));

  const area = (p) => p.split("/").slice(0, 3).join("/");
  const groups = new Map();
  for (const x of entries) {
    const key = area(x.b.path);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(x);
  }

  const clusters = [];
  let cur = { names: [], items: [] };
  for (const [key, items] of [...groups].sort((a, b) => b[1].length - a[1].length)) {
    if (cur.items.length > 0 && cur.items.length + items.length > maxPerCluster) {
      clusters.push(cur);
      cur = { names: [], items: [] };
    }
    cur.names.push(key);
    cur.items.push(...items);
  }
  if (cur.items.length > 0) clusters.push(cur);

  mkdirSync(join(dir, "adrin"), { recursive: true });
  mkdirSync(join(dir, "adrout"), { recursive: true });
  clusters.forEach((c, i) => {
    const name = `cluster-${String(i + 1).padStart(2, "0")}`;
    const text = c.items
      .map(
        ({ e, b }) =>
          `### id=${e.id} ${b.path}:${b.line}\nCOMMENT:\n${b.text}\nCLASSIFIER TOPIC: ${e.topic}\nCLASSIFIER DECISION: ${e.decision}${
            e.adr?.length ? `\nCITES: ${e.adr.join(", ")}` : ""
          }\n`,
      )
      .join("\n");
    writeFileSync(join(dir, "adrin", `${name}.txt`), text);
    process.stdout.write(`${name}: ${c.items.length} decisions (${c.names.slice(0, 4).join(", ")}${c.names.length > 4 ? ", ..." : ""})\n`);
  });
}

function slug(title) {
  return title.toLowerCase().replace(/[^a-z0-9 _-]/g, "").trim().replace(/ /g, "-");
}

function cmdAdrMerge(dir) {
  const root = repoRoot();
  const path = resolve(root, ADR_FILE);
  let text = readFileSync(path, "utf8");
  const eol = text.includes("\r\n") ? "\r\n" : "\n";
  text = text.replace(/\r\n/g, "\n");

  const existing = [...text.matchAll(/^## ADR-(\d+):/gm)].map((m) => parseInt(m[1], 10));
  let next = Math.max(...existing) + 1;
  const rows = [];
  let body = "";
  const files = readdirSync(join(dir, "adrout")).filter((f) => f.endsWith(".md")).sort();
  for (const f of files) {
    const md = readFileSync(join(dir, "adrout", f), "utf8")
      .replace(/\r\n/g, "\n")
      .replace(/\d{4}-\d{2}-\d{2}/, (d) => d)
      .trim()
      .replace(/^## ADR-NEW: (.+)$/gm, (_, title) => {
        const num = String(next++).padStart(3, "0");
        rows.push(`| [ADR-${num}](#${slug(`ADR-${num}: ${title.trim()}`)}) | ${title.trim()} | Accepted |`);
        return `## ADR-${num}: ${title.trim()}`;
      });
    body += "\n" + md + "\n";
  }

  const lastRow = [...text.matchAll(/^\| \[ADR-\d+\]\(.*$/gm)].pop();
  const insertAt = lastRow.index + lastRow[0].length + 1;
  text = text.slice(0, insertAt) + rows.join("\n") + "\n" + text.slice(insertAt);
  const guidance = /append an entry numbered \*\*ADR-\d+\*\*, in the heading form `## ADR-\d+: Title`/;
  const label = String(next).padStart(3, "0");
  text = text.replace(guidance, `append an entry numbered **ADR-${label}**, in the heading form \`## ADR-${label}: Title\``);
  text = text.replace(/\n*$/, "\n") + body;
  writeFileSync(path, text.replace(/\n/g, eol));
  process.stdout.write(`appended ${rows.length} ADRs; next number is ADR-${label}\n`);
}

function cmdIssues(dir, sha) {
  const blocks = readJson(join(dir, "blocks.json"));
  const entries = readJson(join(dir, "cls-all.json")).filter((e) => e.c === "E");
  const groups = new Map();
  for (const e of entries) {
    const key = e.title.trim().toLowerCase();
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(e);
  }
  const remote = execFileSync("git", ["remote", "get-url", "origin"], { encoding: "utf8" })
    .trim()
    .replace(/\.git$/, "")
    .replace(/^git@github\.com:/, "https://github.com/");
  mkdirSync(join(dir, "issues"), { recursive: true });
  let n = 0;
  for (const items of groups.values()) {
    n++;
    const where = items
      .map((e) => {
        const b = blocks[e.id];
        const range = b.endLine > b.line ? `-L${b.endLine}` : "";
        const quoted = b.text.split("\n").map((l) => `  > ${l}`).join("\n");
        return `- [\`${b.path}:${b.line}\`](${remote}/blob/${sha}/${b.path}#L${b.line}${range})\n\n${quoted}`;
      })
      .join("\n\n");
    const body = `A code comment here existed to explain or excuse an awkward implementation. The comment was deleted; this issue tracks fixing the implementation so it needs no excuse.\n\n## The hack\n${[
      ...new Set(items.map((e) => e.hack)),
    ].join("\n\n")}\n\n## Proposed fix\n${[...new Set(items.map((e) => e.fix))].join("\n\n")}\n\n## Original comment${
      items.length > 1 ? "s" : ""
    } (permalinks at the pre-deletion commit)\n${where}\n`;
    writeJson(join(dir, "issues", `${String(n).padStart(2, "0")}.json`), { title: items[0].title, body });
  }
  process.stdout.write(`${n} issue drafts in ${join(dir, "issues")}; merge related drafts, then file with gh issue create\n`);
}

function main() {
  const [command, dirArg, extra] = process.argv.slice(2);
  if (!command || !dirArg) {
    process.stderr.write(
      "usage: comment-audit.mjs chunk <dir> [chunks=13] | merge <dir> | adr-in <dir> [max=100] | adr-merge <dir> | issues <dir> <sha>\n",
    );
    process.exit(2);
  }
  const dir = resolve(dirArg);
  if (command === "chunk") cmdChunk(dir, parseInt(extra ?? "13", 10));
  else if (command === "merge") cmdMerge(dir);
  else if (command === "adr-in") cmdAdrIn(dir, parseInt(extra ?? "100", 10));
  else if (command === "adr-merge") cmdAdrMerge(dir);
  else if (command === "issues") cmdIssues(dir, extra ?? "HEAD");
  else {
    process.stderr.write(`unknown command ${command}\n`);
    process.exit(2);
  }
}

main();
