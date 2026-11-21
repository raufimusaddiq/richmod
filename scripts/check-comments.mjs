#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { basename, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const BASELINE_FILE = "scripts/comment-baseline.json";

const GO_DIRECTIVE = /^\/\/\s*(go:|line |nolint|export |(Unordered output|Output):)/;
const GO_BLOCK_DIRECTIVE = /^\/\*(line )/;
const JS_DIRECTIVE =
  /^(\/\/|\/\*)\s*(eslint-|@ts-|istanbul |c8 |#__PURE__|@vite-ignore|webpackChunkName)/;
const SQL_DIRECTIVE = /^--\s*\+goose\b/;
const HASH_DIRECTIVE = /^#\s*(syntax=|escape=|check=|shellcheck |nolint)/;

function languageOf(path) {
  const base = basename(path);
  const ext = extname(path);
  if (ext === ".go") return "go";
  if (ext === ".js" || ext === ".mjs") return "js";
  if (ext === ".css") return "css";
  if (ext === ".sql") return "sql";
  if (ext === ".sh" || ext === ".yml" || ext === ".yaml") return base.endsWith(".sh") ? "sh" : "yaml";
  if (ext === ".service" || ext === ".timer") return "sh";
  if (/^Dockerfile$|\.Dockerfile$/.test(base)) return "dockerfile";
  return null;
}

function isDirective(lang, text) {
  switch (lang) {
    case "go":
      return GO_DIRECTIVE.test(text) || GO_BLOCK_DIRECTIVE.test(text);
    case "js":
    case "css":
      return JS_DIRECTIVE.test(text);
    case "sql":
      return SQL_DIRECTIVE.test(text);
    default:
      return HASH_DIRECTIVE.test(text);
  }
}

function lineIndex(text) {
  const starts = [0];
  for (let i = 0; i < text.length; i++) if (text[i] === "\n") starts.push(i + 1);
  return starts;
}

function lineAt(starts, offset) {
  let lo = 0;
  let hi = starts.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (starts[mid] <= offset) lo = mid;
    else hi = mid - 1;
  }
  return lo + 1;
}

const REGEX_PRECEDING_KEYWORDS = new Set([
  "return",
  "typeof",
  "instanceof",
  "in",
  "of",
  "new",
  "delete",
  "void",
  "throw",
  "case",
  "do",
  "else",
  "yield",
  "await",
]);

function scanCLike(text, lang) {
  const comments = [];
  const len = text.length;
  const stack = [];
  let i = 0;
  let prev = "";
  let prevWord = "";
  const isJS = lang === "js";
  const isCSS = lang === "css";
  const isGo = lang === "go";

  const push = (start, end, kind) => comments.push({ start, end, kind });

  const scanTemplate = () => {
    while (i < len) {
      const c = text[i];
      if (c === "\\") {
        i += 2;
      } else if (c === "`") {
        i++;
        return;
      } else if (c === "$" && text[i + 1] === "{") {
        i += 2;
        stack.push("tpl");
        return;
      } else {
        i++;
      }
    }
  };

  while (i < len) {
    const c = text[i];
    const n = text[i + 1];

    if (c === "/" && n === "/" && !isCSS) {
      const end = text.indexOf("\n", i);
      const stop = end === -1 ? len : end;
      push(i, text[stop - 1] === "\r" ? stop - 1 : stop, "line");
      i = stop;
      continue;
    }
    if (c === "/" && n === "*") {
      const end = text.indexOf("*/", i + 2);
      const stop = end === -1 ? len : end + 2;
      push(i, stop, "block");
      i = stop;
      continue;
    }

    if (c === '"' || (c === "'" && (isJS || isCSS || isGo))) {
      const quote = c;
      i++;
      while (i < len) {
        const d = text[i];
        if (d === "\\") {
          i += 2;
          continue;
        }
        if (d === quote) {
          i++;
          break;
        }
        if (d === "\n" && !(isGo && false)) break;
        i++;
      }
      prev = "str";
      prevWord = "";
      continue;
    }

    if (c === "'" && !isJS && !isCSS && !isGo) {
      i++;
      continue;
    }

    if (c === "`" && (isJS || isGo)) {
      i++;
      if (isGo) {
        const end = text.indexOf("`", i);
        i = end === -1 ? len : end + 1;
      } else {
        scanTemplate();
      }
      prev = "str";
      prevWord = "";
      continue;
    }

    if (isCSS && c === "u" && text.startsWith("url(", i)) {
      const q = text[i + 4];
      if (q !== '"' && q !== "'") {
        const end = text.indexOf(")", i);
        i = end === -1 ? len : end + 1;
        continue;
      }
    }

    if (isJS && c === "/") {
      const regexAllowed =
        text[i - 1] !== "<" &&
        (prev === "" ||
        (prev === "punct" && ![")", "]", "}"].includes(lastSignificant(text, i))) ||
        (prev === "word" && REGEX_PRECEDING_KEYWORDS.has(prevWord)));
      if (regexAllowed) {
        i++;
        let inClass = false;
        while (i < len) {
          const d = text[i];
          if (d === "\\") {
            i += 2;
            continue;
          }
          if (d === "\n") break;
          if (d === "[") inClass = true;
          else if (d === "]") inClass = false;
          else if (d === "/" && !inClass) {
            i++;
            break;
          }
          i++;
        }
        while (i < len && /[a-z]/i.test(text[i])) i++;
        prev = "str";
        prevWord = "";
        continue;
      }
    }

    if (isJS && c === "{") {
      stack.push("brace");
      i++;
      prev = "punct";
      continue;
    }
    if (isJS && c === "}") {
      const top = stack.pop();
      i++;
      if (top === "tpl") scanTemplate();
      prev = top === "tpl" ? "str" : "punct";
      prevWord = "";
      continue;
    }

    if (/\s/.test(c)) {
      i++;
      continue;
    }

    if (/[A-Za-z0-9_$]/.test(c)) {
      let j = i;
      while (j < len && /[A-Za-z0-9_$]/.test(text[j])) j++;
      prevWord = text.slice(i, j);
      prev = "word";
      i = j;
      continue;
    }

    prev = "punct";
    prevWord = "";
    i++;
  }
  return comments;
}

function lastSignificant(text, from) {
  let j = from - 1;
  while (j >= 0 && /\s/.test(text[j])) j--;
  return j >= 0 ? text[j] : "";
}

function scanSql(text) {
  const comments = [];
  const len = text.length;
  let i = 0;
  while (i < len) {
    const c = text[i];
    const n = text[i + 1];
    if (c === "-" && n === "-") {
      const end = text.indexOf("\n", i);
      const stop = end === -1 ? len : end;
      comments.push({ start: i, end: text[stop - 1] === "\r" ? stop - 1 : stop, kind: "line" });
      i = stop;
      continue;
    }
    if (c === "/" && n === "*") {
      let depth = 1;
      let j = i + 2;
      while (j < len && depth > 0) {
        if (text[j] === "/" && text[j + 1] === "*") {
          depth++;
          j += 2;
        } else if (text[j] === "*" && text[j + 1] === "/") {
          depth--;
          j += 2;
        } else {
          j++;
        }
      }
      comments.push({ start: i, end: j, kind: "block" });
      i = j;
      continue;
    }
    if (c === "'" || c === '"') {
      const quote = c;
      i++;
      while (i < len) {
        if (text[i] === quote) {
          if (text[i + 1] === quote) {
            i += 2;
            continue;
          }
          i++;
          break;
        }
        i++;
      }
      continue;
    }
    i++;
  }
  return comments;
}

function scanHash(text, lang) {
  const comments = [];
  const lines = text.split("\n");
  let offset = 0;
  let heredoc = null;
  for (let ln = 0; ln < lines.length; ln++) {
    const raw = lines[ln];
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    const lineStart = offset;
    offset += raw.length + 1;

    if (heredoc) {
      if (line.trim() === heredoc) heredoc = null;
      continue;
    }
    if (ln === 0 && line.startsWith("#!")) continue;

    let quote = null;
    for (let k = 0; k < line.length; k++) {
      const c = line[k];
      if (quote) {
        if (c === "\\" && quote === '"') k++;
        else if (c === quote) quote = null;
        continue;
      }
      if (c === '"' || c === "'") {
        if (c === "'" && lang === "yaml" && k > 0 && /[A-Za-z]/.test(line[k - 1])) continue;
        quote = c;
        continue;
      }
      if (c === "#") {
        const startsLine = line.slice(0, k).trim() === "";
        const afterSpace = k === 0 || /\s/.test(line[k - 1]);
        if (lang === "dockerfile" && !startsLine) continue;
        if (startsLine || afterSpace) {
          comments.push({ start: lineStart + k, end: lineStart + line.length, kind: "line" });
          break;
        }
      }
      if (c === "<" && line[k + 1] === "<" && (lang === "sh" || lang === "yaml")) {
        const m = /^<<-?\s*(['"]?)([A-Za-z_][A-Za-z0-9_]*)\1/.exec(line.slice(k));
        if (m) {
          heredoc = m[2];
          break;
        }
      }
    }
  }
  return comments;
}

export function scanComments(path, text) {
  const lang = languageOf(path);
  if (!lang) return null;
  const raw =
    lang === "go" || lang === "js" || lang === "css"
      ? scanCLike(text, lang)
      : lang === "sql"
        ? scanSql(text)
        : scanHash(text, lang);
  const starts = lineIndex(text);
  const out = [];
  let outputRunEndLine = 0;
  for (const c of raw) {
    const body = text.slice(c.start, c.end);
    const startLine = lineAt(starts, c.start);
    if (lang === "go" && /^\/\/\s*(Unordered output|Output):/.test(body)) {
      outputRunEndLine = startLine;
      continue;
    }
    if (lang === "go" && outputRunEndLine > 0 && c.kind === "line" && startLine === outputRunEndLine + 1) {
      outputRunEndLine = startLine;
      continue;
    }
    outputRunEndLine = 0;
    if (isDirective(lang, body)) continue;
    const line = startLine;
    const endLine = lineAt(starts, Math.max(c.start, c.end - 1));
    const before = text.slice(starts[line - 1], c.start);
    out.push({
      lang,
      line,
      endLine,
      kind: c.kind,
      trailing: before.trim() !== "",
      start: c.start,
      end: c.end,
      text: body,
    });
  }
  return out;
}

function root() {
  return execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
}

function trackedFiles(cwd) {
  const out = execFileSync("git", ["ls-files", "-z", "--cached", "--others", "--exclude-standard"], { cwd, encoding: "utf8", maxBuffer: 1 << 28 });
  return out.split("\0").filter(Boolean);
}

export function collect(cwd) {
  const files = new Map();
  for (const path of trackedFiles(cwd)) {
    if (!languageOf(path)) continue;
    const comments = scanComments(path, readFileSync(resolve(cwd, path), "utf8"));
    if (comments && comments.length > 0) files.set(path, comments);
  }
  return files;
}

function main() {
  const args = new Set(process.argv.slice(2));
  const cwd = root();
  const files = collect(cwd);

  if (args.has("--extract")) {
    const rows = [];
    for (const [path, comments] of files) for (const c of comments) rows.push({ path, ...c });
    process.stdout.write(JSON.stringify(rows));
    return;
  }

  const counts = Object.fromEntries([...files].map(([p, c]) => [p, c.length]).sort());
  const total = Object.values(counts).reduce((a, b) => a + b, 0);

  if (args.has("--update")) {
    writeFileSync(resolve(cwd, BASELINE_FILE), JSON.stringify(counts, null, 2) + "\n");
    process.stdout.write(`comment baseline written: ${total} comments in ${files.size} files\n`);
    return;
  }

  let baseline = {};
  try {
    baseline = JSON.parse(readFileSync(resolve(cwd, BASELINE_FILE), "utf8"));
  } catch {
    baseline = {};
  }

  const grew = [];
  const shrank = [];
  for (const [path, count] of Object.entries(counts)) {
    const allowed = baseline[path] ?? 0;
    if (count > allowed) grew.push({ path, count, allowed });
    else if (count < allowed) shrank.push({ path, count, allowed });
  }
  for (const [path, allowed] of Object.entries(baseline)) {
    if (!(path in counts)) shrank.push({ path, count: 0, allowed });
  }

  if (grew.length > 0) {
    process.stderr.write("Comments are banned in this repo. Delete these (see .claude/skills/delete-comment/SKILL.md):\n");
    for (const g of grew) {
      process.stderr.write(`  ${g.path}: ${g.count} comments, baseline allows ${g.allowed}\n`);
      for (const c of files.get(g.path) ?? []) {
        process.stderr.write(`    ${g.path}:${c.line}: ${c.text.split("\n")[0].slice(0, 100)}\n`);
      }
    }
    process.exit(1);
  }
  if (shrank.length > 0) {
    process.stderr.write("Comment count dropped below the baseline. Lock in the progress:\n");
    for (const s of shrank) process.stderr.write(`  ${s.path}: ${s.count} < baseline ${s.allowed}\n`);
    process.stderr.write("Run: node scripts/check-comments.mjs --update\n");
    process.exit(1);
  }
  process.stdout.write(`comment ratchet ok: ${total} legacy comments in ${files.size} files\n`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
