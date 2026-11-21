#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { collect, scanComments } from "./check-comments.mjs";

const SENTINEL = "\u0000";

function removeComments(path, text) {
  const comments = scanComments(path, text) ?? [];
  const eol = text.includes("\r\n") ? "\r\n" : "\n";
  const ranges = [];

  for (const c of comments) {
    let start = c.start;
    let end = c.end;
    let lineStart = text.lastIndexOf("\n", start - 1) + 1;
    let lineEndIdx = text.indexOf("\n", end);
    let lineEnd = lineEndIdx === -1 ? text.length : lineEndIdx;

    let before = text.slice(lineStart, start);
    let after = text.slice(end, lineEnd).replace(/\r$/, "");

    if (c.lang === "js" && c.kind === "block" && /\{\s*$/.test(before) && /^\s*\}/.test(after)) {
      start = lineStart + before.lastIndexOf("{");
      end = end + after.indexOf("}") + 1;
      before = text.slice(lineStart, start);
      after = text.slice(end, lineEnd).replace(/\r$/, "");
    }

    if (before.trim() === "" && after.trim() === "") {
      const stop = lineEndIdx === -1 ? text.length : lineEndIdx + 1;
      ranges.push({ start: lineStart, end: stop, replacement: SENTINEL + eol });
    } else if (after.trim() === "") {
      ranges.push({ start: lineStart + before.trimEnd().length, end, replacement: "" });
    } else if (/\s$/.test(before) && /^\s/.test(after)) {
      ranges.push({ start: start - 1, end, replacement: "" });
    } else {
      ranges.push({ start, end, replacement: "" });
    }
  }

  ranges.sort((a, b) => b.start - a.start);
  let out = text;
  for (const r of ranges) out = out.slice(0, r.start) + r.replacement + out.slice(r.end);

  if (!out.includes(SENTINEL)) return out;

  const lines = out.split(eol);
  const isBlank = (l) => l !== undefined && l.trim() === "" && !l.includes(SENTINEL);
  const result = [];
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].includes(SENTINEL)) {
      result.push(lines[i]);
      continue;
    }
    let j = i;
    while (j + 1 < lines.length && lines[j + 1].includes(SENTINEL)) j++;
    const prevBlank = result.length === 0 || isBlank(result[result.length - 1]);
    const nextBlank = j + 1 >= lines.length || isBlank(lines[j + 1]);
    i = j;
    if (prevBlank && nextBlank) {
      if (result.length === 0 || j + 1 >= lines.length) {
        if (result.length > 0) result.pop();
        while (j + 1 < lines.length && isBlank(lines[j + 1])) j++;
        i = j;
      } else {
        i = j + 1;
      }
    }
  }
  return result.join(eol);
}

function skeleton(text, comments, lang) {
  let out = "";
  let at = 0;
  for (const c of comments) {
    out += text.slice(at, c.start);
    at = c.end;
  }
  out += text.slice(at);
  out = out.replace(/\s+/g, "");
  if (lang === "js") out = out.replace(/\{\}/g, "");
  return out;
}

function gofmt(text) {
  const eol = text.includes("\r\n") ? "\r\n" : "\n";
  const lf = text.replace(/\r\n/g, "\n");
  const formatted = execFileSync("gofmt", [], { input: lf, encoding: "utf8", maxBuffer: 1 << 27 });
  return eol === "\r\n" ? formatted.replace(/\n/g, "\r\n") : formatted;
}

function wasGofmtClean(text) {
  try {
    const lf = text.replace(/\r\n/g, "\n");
    return execFileSync("gofmt", ["-l"], { input: lf, encoding: "utf8" }).trim() === "";
  } catch {
    return false;
  }
}

export function deleteComments(cwd, paths) {
  const report = { files: 0, comments: 0, skipped: [] };
  for (const path of paths) {
    const full = resolve(cwd, path);
    const original = readFileSync(full, "utf8");
    const comments = scanComments(path, original);
    if (!comments || comments.length === 0) continue;

    let out = removeComments(path, original);
    if (path.endsWith(".go") && wasGofmtClean(original)) {
      try {
        out = gofmt(out);
      } catch {
        report.skipped.push(`${path}: gofmt failed after edit`);
        continue;
      }
    }

    const remaining = scanComments(path, out) ?? [];
    if (remaining.length > 0) {
      report.skipped.push(`${path}: ${remaining.length} comments survived (line ${remaining[0].line})`);
      continue;
    }
    if (skeleton(original, comments, comments[0].lang) !== skeleton(out, [], comments[0].lang)) {
      report.skipped.push(`${path}: non-comment content changed, refusing to write`);
      continue;
    }

    writeFileSync(full, out);
    report.files++;
    report.comments += comments.length;
  }
  return report;
}

function main() {
  const cwd = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
  const args = process.argv.slice(2);
  const paths = args.length > 0 ? args : [...collect(cwd).keys()];
  const report = deleteComments(cwd, paths);
  process.stdout.write(`deleted ${report.comments} comments in ${report.files} files\n`);
  for (const s of report.skipped) process.stderr.write(`skipped ${s}\n`);
  if (report.skipped.length > 0) process.exit(1);
  process.stdout.write("Now run: node scripts/check-comments.mjs --update\n");
}

import { fileURLToPath } from "node:url";
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
