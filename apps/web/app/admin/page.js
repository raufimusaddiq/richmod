"use client";

import { Children, cloneElement, isValidElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import AppShell from "../components/AppShell";

const tabs = [
  ["overview", "Ringkasan"],
  ["reviews", "Review"],
  ["jobs", "Tugas"],
  ["llm", "LLM"],
  ["logs", "Log"],
  ["households", "Rumah tangga"],
  ["users", "Pengguna"],
  ["audit", "Audit"],
];
const time = (v) =>
  v
    ? new Intl.DateTimeFormat("id-ID", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(v))
    : "—";
const number = (v) => new Intl.NumberFormat("id-ID").format(v || 0);
const percent = (v) => (v == null ? "—" : `${(v * 100).toFixed(1)}%`);
const ms = (v) =>
  v == null
    ? "—"
    : v < 1000
      ? `${Math.round(v)} ms`
      : `${(v / 1000).toFixed(1)} dtk`;

async function get(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error("Tidak dapat memuat data.");
  return response.json();
}
function Badge({ value }) {
  return (
    <span className={`admin-badge admin-${String(value || "").toLowerCase()}`}>
      {badgeLabel[value] || value || "—"}
    </span>
  );
}
const badgeLabel = { ACTIVE: "Aktif", INACTIVE: "Nonaktif", DISABLED: "Dinonaktifkan", PENDING: "Menunggu", RUNNING: "Berjalan", SUCCEEDED: "Berhasil", FAILED: "Gagal", HEALTHY: "Sehat", WARN: "Peringatan", ERROR: "Galat" };
function Metric({ label, value, note }) {
  return (
    <article className="admin-metric">
      <span>{label}</span>
      <b>{value}</b>
      {note && <small>{note}</small>}
    </article>
  );
}
function Empty({ children }) {
  return <p className="empty compact">{children}</p>;
}

export default function AdminPage() {
  const router = useRouter(),
    params = useSearchParams(),
    tab = tabs.some(([key]) => key === params.get("tab"))
      ? params.get("tab")
      : "overview";
  const [me, setMe] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    get("/api/v1/auth/me")
      .then(setMe)
      .catch(() => {
        location.href = "/";
      });
  }, []);
  const select = (key) => router.replace(`/admin?tab=${key}`);
  if (!me) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  return (
    <AppShell
      user={me}
      eyebrow="ADMINISTRASI PLATFORM"
      title="Konsol platform"
    >
      <p className="page-intro">
        Operasi platform. Data sensitif dan isi finansial tidak ditampilkan.
      </p>
      <nav className="admin-tabs" aria-label="Navigasi admin">
        {tabs.map(([key, label]) => (
          <button
            key={key}
            className={tab === key ? "active" : "secondary"}
            onClick={() => select(key)}
          >
            {label}
          </button>
        ))}
      </nav>
      {error && <p className="notice error">{error}</p>}
      <AdminTab tab={tab} setError={setError} />
    </AppShell>
  );
}

function AdminTab({ tab, setError }) {
  if (tab === "overview") return <Overview setError={setError} />;
  if (tab === "reviews") return <Reviews setError={setError} />;
  if (tab === "jobs") return <Jobs setError={setError} />;
  if (tab === "llm") return <LLM setError={setError} />;
  if (tab === "logs") return <Logs setError={setError} />;
  if (tab === "households") return <Households setError={setError} />;
  if (tab === "users") return <Users setError={setError} />;
  return <Audit setError={setError} />;
}
function useDrawerA11y(close) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current?.focus();
    const onKeyDown = (event) => { if (event.key === "Escape") close(); };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [close]);
  return ref;
}

function useLoad(url, setError) {
  const [data, setData] = useState(null);
  const load = useCallback(
    () =>
      get(url)
        .then(setData)
        .catch((err) => setError(err.message)),
    [url, setError],
  );
  useEffect(() => {
    load();
  }, [load]);
  return [data, load];
}
function useAdminList(path, filters, setError) {
  const query = useMemo(() => new URLSearchParams(filters).toString(), [filters]);
  const [data, setData] = useState(null);
  const load = useCallback(() => get(`${path}?${query}`).then(setData).catch((e) => setError(e.message)), [path, query, setError]);
  const more = useCallback(() => {
    if (!data?.nextCursor) return;
    get(`${path}?${query}&cursor=${encodeURIComponent(data.nextCursor)}`).then((next) => setData({ items: [...data.items, ...next.items], nextCursor: next.nextCursor })).catch((e) => setError(e.message));
  }, [data, path, query, setError]);
  useEffect(() => { load(); }, [load]);
  return [data, load, more];
}

function Overview({ setError }) {
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
        <Metric label="Review terbuka" value={number(data.reviews.open)} />
        <Metric label="Rumah tangga" value={number(data.households.total)} />
        <Metric
          label="Cakupan review Telegram"
          value={percent(reviewHealth?.telegramActionableCoverageRate)}
          note="24 jam"
        />
        <Metric
          label="Escape ke Web"
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

function Reviews({ setError }) {
  const [range, setRange] = useState("24h");
  const [filters, setFilters] = useState({
    status: "",
    reviewType: "",
    q: "",
    range: "24h",
  });
  const [summary, refreshSummary] = useLoad(
    `/api/v1/admin/reviews/summary?range=${range}`,
    setError,
  );
  const [breakdown, refreshBreakdown] = useLoad(
    `/api/v1/admin/reviews/breakdown?range=${range}`,
    setError,
  );
  const [projections, refreshProjections, more] = useAdminList(
    "/api/v1/admin/reviews/projections",
    filters,
    setError,
  );
  const refresh = () => {
    refreshSummary();
    refreshBreakdown();
    refreshProjections();
  };
  if (!summary || !breakdown) return <Empty>Memuat review…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">KESEHATAN REVIEW UNIVERSAL</span>
          <h2>Review</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select
          aria-label="Rentang review"
          value={range}
          onChange={(e) => setRange(e.target.value)}
        >
          <option value="1h">1 jam</option>
          <option value="24h">24 jam</option>
          <option value="7d">7 hari</option>
          <option value="30d">30 hari</option>
        </select>
      </div>
      <div className="admin-metrics">
        <Metric label="Review terbuka" value={number(summary.openReviews)} />
        <Metric
          label="Eligible Telegram"
          value={number(summary.eligibleTelegramReviews)}
        />
        <Metric
          label="Proyeksi actionable"
          value={number(summary.actionableTelegramProjections)}
        />
        <Metric
          label="TARC"
          value={percent(summary.telegramActionableCoverageRate)}
          note="Target 100%"
        />
        <Metric
          label="Escape ke Web"
          value={percent(summary.webEscapeRate)}
          note="Target 0%"
        />
        <Metric
          label="Kirim berhasil"
          value={number(summary.deliverySucceeded)}
          note={`${number(summary.deliveryFailed)} gagal`}
        />
        <Metric
          label="Keberhasilan kirim"
          value={percent(summary.deliverySuccessRate)}
          note={`${number(summary.deliveryRetried)} ulang`}
        />
        <Metric
          label="P50 selesai"
          value={ms(summary.resolutionLatencyP50Ms)}
        />
        <Metric
          label="P95 selesai"
          value={ms(summary.resolutionLatencyP95Ms)}
        />
        <Metric
          label="Aksi basi"
          value={number(summary.staleActionAttempts)}
        />
        <Metric
          label="Selesai via Telegram"
          value={number(summary.resolvedByTelegram)}
        />
        <Metric label="Selesai via Web" value={number(summary.resolvedByWeb)} />
        <Metric
          label="Selesai via Sistem"
          value={number(summary.resolvedBySystem)}
        />
      </div>
      <article className="surface admin-panel">
        <div className="section-title">
          <h2>Per jenis review</h2>
        </div>
        <Table
          headers={[
            "Jenis",
            "Dibuat",
            "Terbuka",
            "Eligible",
            "Proyeksi",
            "Cakupan",
            "Telegram",
            "Web",
            "Sistem",
            "Escape",
            "Gagal",
          ]}
        >
          {breakdown.rows?.length ? (
            breakdown.rows.map((row) => (
              <tr key={row.reviewType}>
                <td>{row.reviewType}</td>
                <td>{number(row.created)}</td>
                <td>{number(row.open)}</td>
                <td>{number(row.telegramEligible)}</td>
                <td>{number(row.actionableProjected)}</td>
                <td>{percent(row.coverageRate)}</td>
                <td>{number(row.resolvedTelegram)}</td>
                <td>{number(row.resolvedWeb)}</td>
                <td>{number(row.resolvedSystem)}</td>
                <td>{percent(row.webEscapeRate)}</td>
                <td>{number(row.deliveryFailed)}</td>
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan="11">
                <Empty>Tidak ada data review pada rentang ini.</Empty>
              </td>
            </tr>
          )}
        </Table>
      </article>
      <article className="surface admin-panel">
        <div className="section-title">
          <h2>Operasi proyeksi & pengiriman</h2>
        </div>
        <div className="admin-filters">
          <select
            aria-label="Status proyeksi"
            value={filters.status}
            onChange={(e) => setFilters({ ...filters, status: e.target.value })}
          >
            <option value="">Semua status</option>
            <option value="PENDING_SEND">Menunggu kirim</option>
            <option value="OPEN">Terbuka</option>
            <option value="RESOLVED">Selesai</option>
            <option value="EXPIRED">Kedaluwarsa</option>
            <option value="CANCELLED">Dibatalkan</option>
          </select>
          <input
            aria-label="Jenis review"
            placeholder="Jenis review"
            value={filters.reviewType}
            onChange={(e) =>
              setFilters({ ...filters, reviewType: e.target.value })
            }
          />
          <input
            aria-label="Cari referensi proyeksi"
            placeholder="Cari referensi"
            value={filters.q}
            onChange={(e) => setFilters({ ...filters, q: e.target.value })}
          />
        </div>
        <Table
          headers={[
            "Referensi",
            "Jenis",
            "Status review",
            "Status proyeksi",
            "Pengiriman",
            "Ulang",
            "Umur",
            "Selesai via",
            "Diperbarui",
          ]}
        >
          {projections?.items?.length ? (
            projections.items.map((item) => (
              <tr key={item.projectionId}>
                <td className="admin-id">{item.projectionId}</td>
                <td>{item.reviewType}</td>
                <td>
                  <Badge value={item.reviewStatus} />
                </td>
                <td>
                  <Badge value={item.projectionStatus} />
                </td>
                <td>
                  <Badge value={item.deliveryStatus} />
                </td>
                <td>{number(item.retryCount)}</td>
                <td>{ms(item.ageMs)}</td>
                <td>{item.resolvedSurface || "—"}</td>
                <td>{time(item.updatedAt)}</td>
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan="9">
                <Empty>Tidak ada proyeksi pada rentang ini.</Empty>
              </td>
            </tr>
          )}
        </Table>
        {projections?.nextCursor && (
          <button className="secondary admin-more" onClick={more}>
            Muat berikutnya
          </button>
        )}
      </article>
    </section>
  );
}

function Jobs({ setError }) {
  const [filters, setFilters] = useState({ status: "", lane: "", type: "", range: "24h", q: "" });
  const [data, refresh, more] = useAdminList("/api/v1/admin/jobs", filters, setError), [selected, setSelected] = useState(null);
  if (!data) return <Empty>Memuat jobs…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">ANTREAN TUGAS POSTGRESQL</span>
          <h2>Tugas</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Status tugas" value={filters.status} onChange={(e) => setFilters({...filters, status:e.target.value})}><option value="">Semua status</option><option value="FAILED">Gagal</option><option value="PENDING">Menunggu</option><option value="RUNNING">Berjalan</option><option value="SUCCEEDED">Berhasil</option></select>
        <select aria-label="Jalur tugas" value={filters.lane} onChange={(e) => setFilters({...filters, lane:e.target.value})}><option value="">Semua jalur</option><option value="INTERACTIVE">Interaktif</option><option value="DEFAULT">Bawaan</option><option value="BACKGROUND">Latar belakang</option></select>
        <input aria-label="Jenis job" placeholder="Jenis job" value={filters.type} onChange={(e) => setFilters({...filters, type:e.target.value})} />
        <select aria-label="Rentang job" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
        <input aria-label="Cari Job ID" placeholder="Cari Job ID" value={filters.q} onChange={(e) => setFilters({...filters, q:e.target.value})} />
      </div>
      <Table
        headers={[
          "Status",
          "Type",
          "Lane",
          "Attempts",
          "Durasi",
          "Diperbarui",
          "Job ID",
        ]}
      >
        {data.items.length ? (
          data.items.map((item) => (
            <tr key={item.id}>
              <td>
                <Badge value={item.status} />
              </td>
              <td>{item.type}</td>
              <td>{item.lane}</td>
              <td>
                {item.attempts}/{item.maxAttempts}
              </td>
              <td>
                {item.startedAt && item.finishedAt
                  ? `${Math.round((new Date(item.finishedAt) - new Date(item.startedAt)) / 1000)} dtk`
                  : "—"}
              </td>
              <td>{time(item.updatedAt)}</td>
              <td><button className="admin-link admin-id" onClick={() => setSelected(item.id)}>{item.id}</button></td>
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan="7">
              <Empty>Tidak ada job pada rentang ini.</Empty>
            </td>
          </tr>
        )}
      </Table>
      {data.nextCursor && <button className="secondary admin-more" onClick={more}>Muat berikutnya</button>}
      {selected && (
        <JobDetail
          id={selected}
          close={() => setSelected(null)}
          setError={setError}
        />
      )}
    </section>
  );
}
function JobDetail({ id, close, setError }) {
  const [data] = useLoad(`/api/v1/admin/jobs/${id}`, setError);
  const drawer = useDrawerA11y(close);
  return (
    <aside
      ref={drawer}
      tabIndex={-1}
      className="admin-drawer"
      role="dialog"
      aria-modal="true"
      aria-label="Detail job"
    >
      <button className="secondary" onClick={close}>
        Tutup
      </button>
      {!data ? (
        <Empty>Memuat…</Empty>
      ) : (
        <>
          <h2>Tugas</h2>
          <dl className="admin-definition">
            <dt>ID</dt>
            <dd className="admin-id">{data.id}</dd>
            <dt>Jenis</dt>
            <dd>{data.type}</dd>
            <dt>Jalur</dt>
            <dd>{data.lane}</dd>
            <dt>Status</dt>
            <dd>
              <Badge value={data.status} />
            </dd>
            <dt>Percobaan</dt>
            <dd>
              {data.attempts}/{data.maxAttempts}
            </dd>
            <dt>Dibuat</dt>
            <dd>{time(data.createdAt)}</dd>
            <dt>Mulai</dt>
            <dd>{time(data.startedAt)}</dd>
            <dt>Selesai</dt>
            <dd>{time(data.finishedAt)}</dd>
          </dl>
          <h3>Referensi aman</h3>
          {Object.keys(data.references).length ? (
            <dl className="admin-definition">
              {Object.entries(data.references).map(([k, v]) => (
                <>
                  <dt key={`${k}-key`}>{k}</dt>
                  <dd className="admin-id" key={k}>
                    {v}
                  </dd>
                </>
              ))}
            </dl>
          ) : (
            <Empty>Tidak ada.</Empty>
          )}
          <h3>Riwayat percobaan ulang</h3>
          {data.retries.length ? (
            <Table headers={["#", "Error", "Durasi", "Waktu"]}>
              {data.retries.map((x) => (
                <tr key={x.attempt}>
                  <td>{x.attempt}</td>
                  <td>{x.errorClass}</td>
                  <td>{x.durationMs ?? "—"} ms</td>
                  <td>{time(x.failedAt)}</td>
                </tr>
              ))}
            </Table>
          ) : (
            <Empty>Belum ada retry.</Empty>
          )}
        </>
      )}
    </aside>
  );
}

function LLM({ setError }) {
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

function Logs({ setError }) {
  const [filters, setFilters] = useState({ type: "", severity: "", component: "", range: "24h", q: "" });
  const [data, refresh, more] = useAdminList("/api/v1/admin/logs", filters, setError);
  if (!data) return <Empty>Memuat events…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">KEJADIAN TERSTRUKTUR</span>
          <h2>Log</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Jenis event" value={filters.type} onChange={(e) => setFilters({...filters, type:e.target.value})}><option value="">Semua event</option><option>JOB_RETRY</option><option>JOB_FAILED</option><option>LLM_FAILED</option><option>SOURCE_FAILED</option></select>
        <select aria-label="Keparahan" value={filters.severity} onChange={(e) => setFilters({...filters, severity:e.target.value})}><option value="">Semua tingkat keparahan</option><option value="WARN">Peringatan</option><option value="ERROR">Galat</option></select>
        <input aria-label="Komponen" placeholder="Komponen" value={filters.component} onChange={(e) => setFilters({...filters, component:e.target.value})} />
        <select aria-label="Rentang log" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
        <input aria-label="Reference ID" placeholder="Reference ID" value={filters.q} onChange={(e) => setFilters({...filters, q:e.target.value})} />
      </div>
      <Table
        headers={[
          "Waktu",
          "Keparahan",
          "Kejadian",
          "Komponen",
          "Kelas error",
          "Referensi",
        ]}
      >
        {data.items.length ? (
          data.items.map((x, i) => (
            <tr key={`${x.referenceId}-${i}`}>
              <td>{time(x.createdAt)}</td>
              <td>
                <Badge value={x.severity} />
              </td>
              <td>{x.type}</td>
              <td>{x.component}</td>
              <td>{x.errorClass}</td>
              <td className="admin-id">{x.referenceId}</td>
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan="6">
              <Empty>Belum ada event.</Empty>
            </td>
          </tr>
        )}
      </Table>
      {data.nextCursor && <button className="secondary admin-more" onClick={more}>Muat berikutnya</button>}
    </section>
  );
}

function Households({ setError }) {
  const [data, refresh] = useLoad("/api/v1/admin/households", setError),
    [selected, setSelected] = useState(null);
  if (!data) return <Empty>Memuat household…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">RUMAH TANGGA</span>
          <h2>Rumah tangga</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <Table
        headers={[
          "Household",
          "Members",
          "Transactions",
          "Review",
          "Last activity",
          "Created",
        ]}
      >
        {data.map((x) => (
          <tr key={x.id}>
            <td>
              <b>{x.name}</b>
              <button className="admin-link admin-id" onClick={() => setSelected(x.id)}>{x.id}</button>
            </td>
            <td>{x.members}</td>
            <td>{x.transactions}</td>
            <td>{x.openReviews}</td>
            <td>{time(x.lastActivityAt)}</td>
            <td>{time(x.createdAt)}</td>
          </tr>
        ))}
      </Table>
      {selected && (
        <HouseholdDetail
          id={selected}
          close={() => setSelected(null)}
          setError={setError}
        />
      )}
    </section>
  );
}
function HouseholdDetail({ id, close, setError }) {
  const [data] = useLoad(`/api/v1/admin/households/${id}/overview`, setError);
  const drawer = useDrawerA11y(close);
  return (
    <aside
      ref={drawer}
      tabIndex={-1}
      className="admin-drawer"
      role="dialog"
      aria-modal="true"
      aria-label="Detail household"
    >
      <button className="secondary" onClick={close}>
        Tutup
      </button>
      {!data ? (
        <Empty>Memuat…</Empty>
      ) : (
        <>
          <h2>{data.name}</h2>
          <dl className="admin-definition">
            <dt>ID</dt>
            <dd className="admin-id">{data.id}</dd>
            <dt>Timezone</dt>
            <dd>{data.timezone}</dd>
            <dt>Members</dt>
            <dd>{data.members}</dd>
            <dt>Transactions</dt>
            <dd>{data.transactions}</dd>
            <dt>Open reviews</dt>
            <dd>{data.openReviews}</dd>
            <dt>Bank listeners</dt>
            <dd>{data.integrations.activeBankListeners}</dd>
            <dt>Telegram</dt>
            <dd>{data.integrations.telegramLinked}</dd>
            <dt>Primary salary</dt>
            <dd>
              {data.integrations.primarySalaryConfigured ? "Ya" : "Tidak"}
            </dd>
          </dl>
          {data.reviewDiagnostics && (
            <>
              <h3>Diagnostik review</h3>
              <dl className="admin-definition">
                <dt>Telegram eligible</dt>
                <dd>{data.reviewDiagnostics.eligibleTelegram}</dd>
                <dt>Proyeksi actionable</dt>
                <dd>{data.reviewDiagnostics.actionableProjections}</dd>
                <dt>Selesai via Telegram</dt>
                <dd>{data.reviewDiagnostics.resolvedTelegram}</dd>
                <dt>Selesai via Web</dt>
                <dd>{data.reviewDiagnostics.resolvedWeb}</dd>
                <dt>Selesai via Sistem</dt>
                <dd>{data.reviewDiagnostics.resolvedSystem}</dd>
                <dt>Gagal kirim terakhir</dt>
                <dd>
                  {data.reviewDiagnostics.latestDeliveryFailureAt
                    ? time(data.reviewDiagnostics.latestDeliveryFailureAt)
                    : "—"}
                </dd>
                <dt>Kelas galat</dt>
                <dd>
                  {data.reviewDiagnostics.latestDeliveryFailureErrorClass ||
                    "—"}
                </dd>
              </dl>
            </>
          )}
          <h3>Anggota</h3>
          {data.memberItems?.length ? <Table headers={["Nama", "Email", "Role", "Status"]}>{data.memberItems.map((x) => <tr key={x.id}><td>{x.displayName}</td><td>{x.email}</td><td>{x.role}</td><td><Badge value={x.active ? "ACTIVE" : "INACTIVE"} /></td></tr>)}</Table> : <Empty>Tidak ada anggota.</Empty>}
          <h3>LLM terbaru</h3>
          {data.recentLLMCalls?.length ? <Table headers={["Waktu", "Task", "Status", "Latency"]}>{data.recentLLMCalls.map((x) => <tr key={x.id}><td>{time(x.createdAt)}</td><td>{x.task}</td><td><Badge value={x.status} /></td><td>{x.durationMs} ms</td></tr>)}</Table> : <Empty>Tidak ada panggilan LLM.</Empty>}
          <h3>Source gagal</h3>
          {data.failedSourceEvents?.length ? <Table headers={["Waktu", "Type", "ID"]}>{data.failedSourceEvents.map((x) => <tr key={x.id}><td>{time(x.createdAt)}</td><td>{x.sourceType}</td><td className="admin-id">{x.id}</td></tr>)}</Table> : <Empty>Tidak ada source gagal.</Empty>}
          <h3>Audit terbaru</h3>
          {data.recentAudit?.length ? <Table headers={["Waktu", "Action", "Entity"]}>{data.recentAudit.map((x) => <tr key={x.id}><td>{time(x.createdAt)}</td><td>{x.action}</td><td>{x.entityType} · <span className="admin-id">{x.entityId}</span></td></tr>)}</Table> : <Empty>Belum ada audit.</Empty>}
        </>
      )}
    </aside>
  );
}

function Users({ setError }) {
  const [users, refresh] = useLoad("/api/v1/admin/users", setError);
  const mutate = async (user, patch, label) => {
    if (!confirm(`${label} ${user.email}?`)) return;
    try {
      const response = await fetch(`/api/v1/admin/users/${user.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
      if (!response.ok)
        throw new Error("Perubahan ditolak. Periksa invariant administrator.");
      refresh();
    } catch (err) {
      setError(err.message);
    }
  };
  if (!users) return <Empty>Memuat users…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">IDENTITAS</span>
          <h2>Pengguna</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <Table
        headers={[
          "Pengguna",
          "Email",
          "Status",
          "Rumah tangga",
          "Kata sandi",
          "Peran",
          "Aksi",
        ]}
      >
        {users.map((user) => (
          <tr key={user.id}>
            <td>{user.displayName}</td>
            <td>{user.email}</td>
            <td>
              <Badge value={user.active ? "ACTIVE" : "INACTIVE"} />
            </td>
            <td>{user.households}</td>
            <td>{user.passwordInitialized ? "Diatur" : "Belum"}</td>
            <td>{user.isSuperAdmin ? "Super Admin" : "User"}</td>
            <td className="admin-actions">
              <button
                className="secondary"
                onClick={() =>
                  mutate(
                    user,
                    { active: !user.active },
                    user.active ? "Nonaktifkan" : "Aktifkan",
                  )
                }
              >
                {user.active ? "Nonaktifkan" : "Aktifkan"}
              </button>
              <button
                className="secondary"
                onClick={() =>
                  mutate(
                    user,
                    { isSuperAdmin: !user.isSuperAdmin },
                    user.isSuperAdmin
                      ? "Cabut Super Admin dari"
                      : "Jadikan Super Admin",
                  )
                }
              >
                {user.isSuperAdmin ? "Cabut admin" : "Jadikan admin"}
              </button>
            </td>
          </tr>
        ))}
      </Table>
    </section>
  );
}

function Audit({ setError }) {
  const [kind, setKind] = useState("all"), [householdId, setHouseholdId] = useState(""), [filters, setFilters] = useState({ action: "", range: "24h" });
  const platform = kind === "platform";
  const household = kind === "household";
  const path = platform ? "/api/v1/admin/audit/platform" : household ? "/api/v1/admin/audit/household" : "/api/v1/admin/audit/all";
  const [data, refresh, more] = useAdminList(path, household ? {...filters, householdId} : filters, setError);
  if (!data) return <Empty>Memuat audit…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">KEJADIAN AUDIT PERMANEN</span>
          <h2>Audit</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Jenis audit" value={kind} onChange={(e) => setKind(e.target.value)}><option value="all">Semua</option><option value="platform">Platform</option><option value="household">Rumah tangga</option></select>
        {household && <input aria-label="ID household" placeholder="ID household" value={householdId} onChange={(e) => setHouseholdId(e.target.value)} />}
        <input aria-label="Tindakan audit" placeholder="Tindakan" value={filters.action} onChange={(e) => setFilters({...filters, action:e.target.value})} />
        <select aria-label="Rentang audit" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
      </div>
      {household && !householdId ? <Empty>Masukkan ID household untuk melihat audit scoped.</Empty> : <><Table headers={["Waktu", "Action", "Actor", "Entity", "Ringkasan"]}>
        {data.items.length ? (
          data.items.map((x) => (
            <tr key={x.id}>
              <td>{time(x.createdAt)}</td>
              <td>{x.action}</td>
              <td>{x.actorEmail || "—"}</td>
              <td>
                {x.entityType} · <span className="admin-id">{x.entityId}</span>
              </td>
              <td>
                <AuditSummary item={x} />
              </td>
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan="5">
              <Empty>Belum ada event audit pada rentang ini.</Empty>
            </td>
          </tr>
        )}
      </Table>{data.nextCursor && <button className="secondary admin-more" onClick={more}>Muat berikutnya</button>}</>}
    </section>
  );
}
function AuditSummary({ item }) {
  const values = Object.entries(item.metadata || {}).filter(([, value]) => typeof value === "string" || typeof value === "boolean" || typeof value === "number");
  return <span>{values.length ? values.map(([key, value]) => `${key}: ${value}`).join(" · ") : "Perubahan administratif tercatat."}{item.requestId ? <small className="admin-id">Request {item.requestId}</small> : null}</span>;
}
function Table({ headers, children }) {
  const rows = Children.map(children, (row) => {
    if (!isValidElement(row)) return row;
    const cells = Children.map(row.props.children, (cell, index) => {
      if (!isValidElement(cell) || cell.props.colSpan) return cell;
      return cloneElement(cell, { "data-label": headers[index] || "" });
    });
    return cloneElement(row, undefined, cells);
  });
  return (
    <div className="admin-table-wrap">
      <table className="admin-table">
        <thead>
          <tr>
            {headers.map((x) => (
              <th key={x}>{x}</th>
            ))}
          </tr>
        </thead>
        <tbody>{rows}</tbody>
      </table>
    </div>
  );
}
