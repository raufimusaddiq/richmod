import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8").replaceAll("\r", "");

test("route entry uses a scoped view transition and never transforms the template wrapper", () => {
  assert.ok(text("app/components/AppShell.js").includes('<ViewTransition name="app-main" enter="page-swap" exit="page-swap" share="page-swap" default="none"><main'));
  assert.ok(text("app/template.js").includes('<div className="route-fade">{children}</div>'));
  const motion = text("app/styles/15-motion.css");
  assert.ok(motion.includes("::view-transition-new(.page-swap) { animation: page-enter var(--motion-page)"));
  assert.doesNotMatch(motion, /\.route-fade \{[^}]*transform/);
});

test("the single reduced-motion guard also silences view-transition pseudo-elements", () => {
  const motion = text("app/styles/15-motion.css");
  const guard = motion.slice(motion.indexOf("@media (prefers-reduced-motion: reduce)"), motion.indexOf("@keyframes row-in"));
  assert.ok(guard.includes("::view-transition-old(*)"));
  assert.ok(guard.includes("::view-transition-new(*)"));
});

test("drawers and unmounting dialogs leave through Presence so exits animate", () => {
  for (const file of ["app/transactions/page.js", "app/documents/page.js", "app/wealth/WealthWorkspace.js", "app/analytics/page.js", "app/admin/Households.js", "app/admin/Jobs.js", "app/components/useDialogs.js", "app/components/QuickRecordProvider.js"]) {
    assert.ok(text(file).includes("<Presence show="), `${file} wraps its overlay in Presence`);
  }
  const motion = text("app/styles/15-motion.css");
  assert.ok(motion.includes('.presence[data-presence="leaving"] :is(.detail-drawer, .admin-drawer)'));
  assert.ok(motion.includes("@media (max-width: 680px) {\n  .detail-drawer, .admin-drawer { animation-name: sheet-in; }"));
});
