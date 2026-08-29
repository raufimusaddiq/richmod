import assert from "node:assert/strict";
import test from "node:test";
import { completenessLabel, insightQuality, pollInsight, selectCycleInsight } from "../app/lib/insightData.js";

test("completeness labels use Indonesian thresholds", () => {
  assert.equal(completenessLabel("0.96"), "Tinggi");
  assert.equal(completenessLabel("0.82"), "Cukup");
  assert.equal(completenessLabel("0.64"), "Perlu dilengkapi");
});

test("cycle insight matches active cycle, not array position", () => {
  const insight = selectCycleInsight([
    { id: "old", status: "SUCCEEDED", createdAt: "2026-08-29T10:00:00Z", metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-07-25" } },
    { id: "current", status: "PENDING", createdAt: "2026-08-29T09:00:00Z", metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-08-25" } },
  ], { cycleStart: "2026-08-25" });
  assert.equal(insight.id, "current");
  assert.equal(selectCycleInsight([], { cycleStart: "2026-08-25" }), null);
});

test("insight quality accepts API strings", () => {
  assert.deepEqual(insightQuality({ dataCompleteness: "0.96" }), { value: 0.96, label: "Tinggi" });
});

test("polling stops on success and failure", async () => {
  let calls = 0;
  const succeeded = await pollInsight({ insightId: "current", attempts: 3, wait: async () => {}, load: async () => { calls += 1; return [{ id: "current", status: calls === 2 ? "SUCCEEDED" : "PENDING" }]; } });
  assert.equal(succeeded.status, "SUCCEEDED");
  assert.equal(calls, 2);
  calls = 0;
  const failed = await pollInsight({ insightId: "current", load: async () => { calls += 1; return [{ id: "current", status: "FAILED" }]; } });
  assert.equal(failed.status, "FAILED");
  assert.equal(calls, 1);
});

test("polling is bounded and honors cancellation", async () => {
  let calls = 0;
  await assert.rejects(pollInsight({ insightId: "missing", attempts: 2, wait: async () => {}, load: async () => { calls += 1; return []; } }), /insight polling timeout/);
  assert.equal(calls, 2);
  const controller = new AbortController();
  controller.abort();
  await assert.rejects(pollInsight({ insightId: "missing", signal: controller.signal, load: async () => [] }), error => error.name === "AbortError");
});
