"use client";

import { useState } from "react";
import { time, number, ms, Badge, Metric, Empty, useLoad, useAdminList, Table } from "./shared";

export default function LLM({ setError }) {
  const [filters, setFilters] = useState({ range: "24h", task: "", status: "" });
  const [summary, refreshSummary] = useLoad(`/api/v1/admin/llm/summary?range=${filters.range}`, setError);
  const [calls, refreshCalls, more] = useAdminList("/api/v1/admin/llm/calls", filters, setError);
  const refresh = () => { refreshSummary(); refreshCalls(); };
  if (!summary || !calls) return <Empty>Memuat LLM…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">GERBANG LLM CLOUD</span>
          <h2>LLM</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Rentang LLM" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
        <input aria-label="Tugas LLM" placeholder="Tugas" value={filters.task} onChange={(e) => setFilters({...filters, task:e.target.value})} />
        <select aria-label="Status LLM" value={filters.status} onChange={(e) => setFilters({...filters, status:e.target.value})}><option value="">Semua status</option><option>SUCCEEDED</option><option>FAILED</option></select>
      </div>
      <div className="admin-metrics">
        <Metric label="Panggilan 24 jam" value={number(summary.calls)} />
        <Metric
          label="Tingkat keberhasilan"
          value={
            summary.successRate == null
              ? "—"
              : `${(summary.successRate * 100).toFixed(1)}%`
          }
        />
        <Metric
          label="P95"
          value={
            summary.p95DurationMs == null
              ? "—"
              : `${Math.round(summary.p95DurationMs)} ms`
          }
        />
        <Metric
          label="Token"
          value={number(
            (summary.inputTokens || 0) + (summary.outputTokens || 0),
          )}
        />
      </div>
      <article className="surface admin-panel">
        <div className="section-title">
          <h2>Rincian tugas</h2>
        </div>
        <Table
          headers={["Tugas", "Panggilan", "Gagal", "P50", "P95", "Token"]}
        >
          {summary.tasks.map((x) => (
            <tr key={x.task}>
              <td>{x.task}</td>
              <td>{x.calls}</td>
              <td>{x.failed}</td>
              <td>
                {x.p50DurationMs == null
                  ? "—"
                  : `${Math.round(x.p50DurationMs)} ms`}
              </td>
              <td>
                {x.p95DurationMs == null
                  ? "—"
                  : `${Math.round(x.p95DurationMs)} ms`}
              </td>
              <td>{number(x.tokens)}</td>
            </tr>
          ))}
        </Table>
        {calls.nextCursor && <button className="secondary admin-more" onClick={more}>Muat berikutnya</button>}
      </article>
      <article className="surface admin-panel">
        <div className="section-title">
          <h2>Panggilan terbaru</h2>
        </div>
        <Table
          headers={[
            "Waktu",
            "Task",
            "Protocol",
            "Model",
            "Status",
            "Latency",
            "Tokens",
          ]}
        >
          {calls.items.length ? (
            calls.items.map((x) => (
              <tr key={x.id}>
                <td>{time(x.createdAt)}</td>
                <td>{x.task}</td>
                <td>{x.protocol}</td>
                <td>{x.model || "—"}</td>
                <td>
                  <Badge value={x.status} />
                </td>
                <td>{x.durationMs} ms</td>
                <td>{number(x.inputTokens + x.outputTokens)}</td>
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan="7">
                <Empty>Belum ada panggilan LLM.</Empty>
              </td>
            </tr>
          )}
        </Table>
      </article>
    </section>
  );
}
