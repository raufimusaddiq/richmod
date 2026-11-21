import assert from "node:assert/strict";
import test from "node:test";
import { scanComments } from "./check-comments.mjs";

const texts = (path, source) => scanComments(path, source).map((c) => c.text);

test("go: line and block comments are found, strings and runes are not", () => {
  const source = ['package x', 'var a = "// not a comment"', "var b = `/* raw */`", "var c = '/'", "// real", "/* block */", ""].join("\n");
  assert.deepEqual(texts("a.go", source), ["// real", "/* block */"]);
});

test("go: tool directives are not counted", () => {
  const source = ["//go:build linux", "//go:embed assets", "//nolint:errcheck", "// plain", ""].join("\n");
  assert.deepEqual(texts("a.go", source), ["// plain"]);
});

test("js: regex literals, template literals and JSX closing tags do not hide comments", () => {
  const source = [
    "const r = /\\/\\//g;",
    "const t = `a ${ x // inline",
    "} // not a comment`;",
    "const el = <p>hi</p>;",
    "// after jsx",
    "",
  ].join("\n");
  assert.deepEqual(texts("a.js", source), ["// inline", "// after jsx"]);
});

test("js: jsx expression comments and eslint directives", () => {
  const source = ["const a = <div>{/* note */}</div>;", "// eslint-disable-next-line no-unused-vars", ""].join("\n");
  assert.deepEqual(texts("a.js", source), ["/* note */"]);
});

test("css: only block comments, url() contents are skipped", () => {
  const source = [".a { background: url(http://example.com/x.png); }", "/* note */", ""].join("\n");
  assert.deepEqual(texts("a.css", source), ["/* note */"]);
});

test("sql: dashes inside strings are not comments and goose directives are kept", () => {
  const source = ["-- +goose Up", "SELECT '--x';", "-- why", "/* block */", ""].join("\n");
  assert.deepEqual(texts("a.sql", source), ["-- why", "/* block */"]);
});

test("shell and yaml: hash inside words and quotes is not a comment", () => {
  assert.deepEqual(texts("a.sh", ['#!/usr/bin/env sh', 'echo "a # b" ${#x} a#b', "# note", ""].join("\n")), ["# note"]);
  assert.deepEqual(texts("a.yml", ["key: value # trailing", 'url: "http://x/#frag"', ""].join("\n")), ["# trailing"]);
});

test("trailing flag and line numbers", () => {
  const found = scanComments("a.go", ["package x", "var a = 1 // why", "// own line", ""].join("\n"));
  assert.deepEqual(
    found.map((c) => [c.line, c.trailing]),
    [
      [2, true],
      [3, false],
    ],
  );
});
