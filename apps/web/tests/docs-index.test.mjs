import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { buildAdrIndex, escapeCell, readStatus } from "../../../scripts/generate-adr-index.mjs";

test("a table cell escapes the backslash before the pipe, so the escaping cannot be undone", () => {
  assert.equal(escapeCell("plain"), "plain");
  assert.equal(escapeCell("a|b"), "a\\|b");
  assert.equal(escapeCell("a\\b"), "a\\\\b");
  assert.equal(escapeCell("a\\|b"), "a\\\\\\|b", "an existing backslash before a pipe stays a literal backslash followed by an escaped pipe");
  assert.equal(escapeCell("\\"), "\\\\");
});

test("the ADR index is generated from the records and is up to date", () => {
  const committed = readFileSync(new URL("../../../docs/adr/README.md", import.meta.url), "utf8").replace(/\r\n/g, "\n");
  assert.equal(committed, buildAdrIndex(), "docs/adr/README.md is out of date; run: node scripts/generate-adr-index.mjs");
});

test("the ADR index lists every record and discloses the shared ADR-033 number", () => {
  const index = buildAdrIndex();
  assert.match(index, /\| \[ADR-049\]\(ADR-049-failed-sources-surface-as-actions\.md\) \|/);
  assert.match(index, /\*\*Duplicate number:\*\* ADR-033 is used by two records/);
  assert.equal((index.match(/^\| \[ADR-033\]/gm) || []).length, 2, "both ADR-033 records are listed");
});

test("an ADR Status is read whole, from either heading or bullet form", () => {
  const multiline = "# ADR-1: x\n\n## Status\n\nAccepted for staged implementation. Existing extraction remains the primary\nfinancial-state path until the documented rollout gate is met.\n\n## Decision\n\nText.";
  assert.equal(readStatus(multiline), "Accepted for staged implementation. Existing extraction remains the primary financial-state path until the documented rollout gate is met.");
  assert.equal(readStatus("# ADR-2: y\n\n- Status: Accepted\n- Date: 2026-09-06\n\n## Decision\n"), "Accepted");
  assert.equal(readStatus("# ADR-3: z\n\n## Decision\n\nNo status recorded.\n"), "");
  assert.equal(readStatus("# ADR-4: w\r\n\r\n## Status\r\n\r\nAccepted.\r\n\r\n## Decision\r\n"), "Accepted.", "CRLF files read the same");
});
