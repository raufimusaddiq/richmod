#!/usr/bin/env node
// Generates docs/adr/README.md from each ADR's title and Status.
//
//   node scripts/generate-adr-index.mjs           write docs/adr/README.md
//   node scripts/generate-adr-index.mjs --check   exit 1 if the file is out of date
//
// apps/web/tests/docs-index.test.mjs runs the check in CI, so the index cannot
// drift when an ADR is added or its Status changes.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// A Markdown table cell: escape the backslash first, then the pipe, so the escaping
// cannot be undone by a backslash that is already in the text.
export function escapeCell(value) {
  return value.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
}

// The Status is one of three shapes in this repository:
//   "## Status" heading followed by a paragraph (possibly several lines),
//   a "- Status: ..." bullet near the top, or no Status at all.
export function readStatus(text) {
  const lines = text.split(/\r?\n/);
  const heading = lines.findIndex(line => /^##\s+Status\s*$/.test(line));
  if (heading >= 0) {
    const paragraph = [];
    for (let i = heading + 1; i < lines.length; i++) {
      const line = lines[i];
      if (/^#{1,6}\s/.test(line)) break;
      if (!line.trim()) {
        if (paragraph.length) break; // the first paragraph only
        continue;
      }
      paragraph.push(line.trim());
    }
    return paragraph.join(" ").replace(/\s+/g, " ").trim();
  }
  const bullet = lines.slice(0, 12).map(line => line.match(/^[-*]\s+\**Status:?\**:?\s*(.+)$/i)).find(Boolean);
  return bullet ? bullet[1].replace(/\s+/g, " ").trim() : "";
}

export function buildAdrIndex(adrDir = path.join(repoRoot, "docs", "adr")) {
  const files = fs.readdirSync(adrDir).filter(name => /^ADR-\d+-.*\.md$/.test(name)).sort();
  const rows = files.map(file => {
    const text = fs.readFileSync(path.join(adrDir, file), "utf8");
    const number = Number(file.match(/^ADR-(\d+)/)[1]);
    const heading = (text.match(/^# (.+)$/m) || [null, file])[1].replace(/^ADR-\d+\s*[:—–-]\s*/, "").trim();
    return { file, number, title: heading, status: readStatus(text) };
  });
  const counts = new Map();
  for (const row of rows) counts.set(row.number, (counts.get(row.number) || 0) + 1);
  const duplicates = [...counts].filter(([, count]) => count > 1).map(([number]) => number);
  const pad = number => String(number).padStart(3, "0");
  const next = Math.max(...rows.map(row => row.number)) + 1;

  const out = [
    "# Architecture decision records",
    "",
    "An ADR records a decision that changes architecture, infrastructure, or a security boundary. `AGENTS.md` requires one before any material architecture change or new infrastructure. Product decisions live in [`../bdr/`](../bdr/) as Business Decision Records.",
    "",
    `- **Adding one:** use the next number, **ADR-${pad(next)}**, and the file name \`ADR-${pad(next)}-short-title.md\` with Context, Decision and Status sections like the existing records.`,
    "- **Current versus superseded:** the Status line of each record is authoritative. Where an ADR was amended, the Status names the amending ADR; read both.",
  ];
  if (duplicates.length) {
    out.push(`- **Duplicate number:** ${duplicates.map(number => `ADR-${pad(number)}`).join(", ")} is used by two records on different topics. Both are current. References elsewhere in the docs use the full file name, so this index lists both rows rather than renaming either.`);
  }
  out.push(
    "",
    "This table is generated from each file's title and Status by `scripts/generate-adr-index.mjs`; run `node scripts/generate-adr-index.mjs` after adding an ADR or changing a Status. A web unit test fails when it is out of date.",
    "",
    "| ADR | Title | Status |",
    "| --- | --- | --- |",
  );
  for (const row of rows) out.push(`| [ADR-${pad(row.number)}](${row.file}) | ${escapeCell(row.title)} | ${escapeCell(row.status) || "Not recorded"} |`);
  out.push("");
  return out.join("\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const target = path.join(repoRoot, "docs", "adr", "README.md");
  const generated = buildAdrIndex();
  if (process.argv.includes("--check")) {
    const current = fs.existsSync(target) ? fs.readFileSync(target, "utf8").replace(/\r\n/g, "\n") : "";
    if (current !== generated) {
      console.error("docs/adr/README.md is out of date. Run: node scripts/generate-adr-index.mjs");
      process.exit(1);
    }
    console.log("docs/adr/README.md is up to date");
  } else {
    fs.writeFileSync(target, generated);
    console.log(`wrote ${path.relative(repoRoot, target)}`);
  }
}
