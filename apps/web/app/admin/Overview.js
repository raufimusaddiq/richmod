"use client";

import { time, number, percent, Badge, Metric, Empty, useLoad, Table } from "./shared";

export default function Overview({ setError }) {
  const [data, refresh] = useLoad("/api/v1/admin/overview", setError);
  const [reviewHealth] = useLoad(
    "/api/v1/admin/reviews/summary?range=24h",
    setError,
  );
  if (!data) return <Empty>Memuat ringkasan…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">STATUS PLATFORM</span>
          <h2>Ringkasan</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-metrics">
        <Metric
          label="Platform"
          value={<Badge value={data.status} />}
          note={`Dicek ${time(data.checkedAt)}`}
        />
        <Metric
          label="Pemroses latar"
          value={data.worker.healthy ? "Sehat" : "Perlu cek"}
          note={
            data.worker.lastHeartbeatAt
              ? `Terlihat ${time(data.worker.lastHeartbeatAt)}`
              : "Belum ada sinyal kesehatan"
          }
        />
        <Metric
          label="Antrean"
          value={`${number(data.jobs.pending)} menunggu`}
          note={`${number(data.jobs.running)} berjalan`}
        />
        <Metric
          label="Gagal 24 jam"
          value={number(data.jobs.failed24h)}
          note="Job terminal"
        />
        <Metric
          label="LLM 24 jam"
          value={number(data.llm.calls24h)}
          note={`${number(data.llm.failed24h)} gagal`}
        />
        <Metric
          label="Success LLM"
          value={
            data.llm.successRate == null
              ? "—"
              : `${(data.llm.successRate * 100).toFixed(1)}%`
          }
          note="24 jam"
        />
        <Metric label="Tinjauan terbuka" value={number(data.reviews.open)} />
        <Metric label="Rumah tangga" value={number(data.households.total)} />
        <Metric
          label="Cakupan review Telegram"
          value={percent(reviewHealth?.telegramActionableCoverageRate)}
          note="24 jam"
        />
        <Metric
          label="Dialihkan ke web"
          value={percent(reviewHealth?.webEscapeRate)}
          note="24 jam"
        />
        <Metric
          label="Kirim review gagal"
          value={number(reviewHealth?.deliveryFailed)}
          note="24 jam"
        />
      </div>
      <div className="admin-grid">
        <article className="surface admin-panel">
          <div className="section-title">
            <h2>Antrean per jalur</h2>
          </div>
          {data.jobs.lanes.map((l) => (
            <div className="admin-lane" key={l.lane}>
              <b>{l.lane}</b>
              <span>
                {l.pending} menunggu · {l.running} berjalan
              </span>
              <small>
                {l.oldestDueAgeMs == null
                  ? "Tidak ada job jatuh tempo"
                  : `Tertua ${Math.round(l.oldestDueAgeMs / 1000)} dtk`}
              </small>
            </div>
          ))}
        </article>
        <article className="surface admin-panel">
          <div className="section-title">
            <h2>LLM 24 jam</h2>
          </div>
          <dl className="admin-definition">
            <dt>Tingkat keberhasilan</dt>
            <dd>
              {data.llm.successRate == null
                ? "—"
                : `${(data.llm.successRate * 100).toFixed(1)}%`}
            </dd>
            <dt>P95</dt>
            <dd>
              {data.llm.p95DurationMs == null
                ? "—"
                : `${Math.round(data.llm.p95DurationMs)} ms`}
            </dd>
            <dt>Token</dt>
            <dd>
              {number(
                (data.llm.inputTokens || 0) + (data.llm.outputTokens || 0),
              )}
            </dd>
            <dt>Gerbang LLM</dt>
            <dd>
              {data.integrations.llmGatewayConfigured
                ? data.integrations.llmProtocol
                : "Tidak dikonfigurasi"}
            </dd>
          </dl>
        </article>
      </div>
      <article className="surface admin-panel">
        <div className="section-title"><h2>Kejadian operasional terbaru</h2></div>
        {data.recentEvents?.length ? <Table headers={["Waktu", "Keparahan", "Kejadian", "Komponen", "Referensi"]}>{data.recentEvents.map((x, i) => <tr key={`${x.referenceId}-${i}`}><td>{time(x.createdAt)}</td><td><Badge value={x.severity} /></td><td>{x.type}</td><td>{x.component}</td><td className="admin-id">{x.referenceId}</td></tr>)}</Table> : <Empty>Belum ada kejadian operasional.</Empty>}
      </article>
    </section>
  );
}
