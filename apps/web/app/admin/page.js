"use client";

import { Children, cloneElement, isValidElement, useCallback, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import AppShell from "../components/AppShell";

const tabs = [
  ["overview", "Overview"],
  ["jobs", "Jobs"],
  ["llm", "LLM"],
  ["logs", "Logs"],
  ["households", "Households"],
  ["users", "Users"],
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

async function get(url) {
  const response = await fetch(url);
  if (!response.ok) throw new Error("Tidak dapat memuat data.");
  return response.json();
}
function Badge({ value }) {
  return (
    <span className={`admin-badge admin-${String(value || "").toLowerCase()}`}>
      {value || "—"}
    </span>
  );
}
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
  if (!me) return <main className="loading">Memuat…</main>;
  return (
    <AppShell
      user={me}
      eyebrow="ADMINISTRASI PLATFORM"
      title="Platform Console"
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
  if (tab === "jobs") return <Jobs setError={setError} />;
  if (tab === "llm") return <LLM setError={setError} />;
  if (tab === "logs") return <Logs setError={setError} />;
  if (tab === "households") return <Households setError={setError} />;
  if (tab === "users") return <Users setError={setError} />;
  return <Audit setError={setError} />;
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
  if (!data) return <Empty>Memuat overview…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">STATUS PLATFORM</span>
          <h2>Overview</h2>
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
          label="Worker"
          value={data.worker.healthy ? "Sehat" : "Perlu cek"}
          note={
            data.worker.lastHeartbeatAt
              ? `Terlihat ${time(data.worker.lastHeartbeatAt)}`
              : "Belum ada heartbeat"
          }
        />
        <Metric
          label="Antrean"
          value={`${number(data.jobs.pending)} pending`}
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
        <Metric label="Review terbuka" value={number(data.reviews.open)} />
        <Metric label="Household" value={number(data.households.total)} />
      </div>
      <div className="admin-grid">
        <article className="surface admin-panel">
          <div className="section-title">
            <h2>Antrean per lane</h2>
          </div>
          {data.jobs.lanes.map((l) => (
            <div className="admin-lane" key={l.lane}>
              <b>{l.lane}</b>
              <span>
                {l.pending} pending · {l.running} berjalan
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
            <dt>Success rate</dt>
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
            <dt>Biaya</dt>
            <dd>{data.llm.cost ?? "—"}</dd>
            <dt>Gateway</dt>
            <dd>
              {data.integrations.llmGatewayConfigured
                ? data.integrations.llmProtocol
                : "Tidak dikonfigurasi"}
            </dd>
          </dl>
        </article>
      </div>
      <article className="surface admin-panel">
        <div className="section-title"><h2>Event operasional terbaru</h2></div>
        {data.recentEvents?.length ? <Table headers={["Waktu", "Severity", "Event", "Komponen", "Referensi"]}>{data.recentEvents.map((x, i) => <tr key={`${x.referenceId}-${i}`}><td>{time(x.createdAt)}</td><td><Badge value={x.severity} /></td><td>{x.type}</td><td>{x.component}</td><td className="admin-id">{x.referenceId}</td></tr>)}</Table> : <Empty>Belum ada event operasional.</Empty>}
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
          <span className="eyebrow">POSTGRESQL JOB QUEUE</span>
          <h2>Jobs</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Status job" value={filters.status} onChange={(e) => setFilters({...filters, status:e.target.value})}><option value="">Semua status</option><option>FAILED</option><option>PENDING</option><option>RUNNING</option><option>SUCCEEDED</option></select>
        <select aria-label="Lane job" value={filters.lane} onChange={(e) => setFilters({...filters, lane:e.target.value})}><option value="">Semua lane</option><option>INTERACTIVE</option><option>DEFAULT</option><option>BACKGROUND</option></select>
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
  return (
    <aside
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
          <h2>Job</h2>
          <dl className="admin-definition">
            <dt>ID</dt>
            <dd className="admin-id">{data.id}</dd>
            <dt>Type</dt>
            <dd>{data.type}</dd>
            <dt>Lane</dt>
            <dd>{data.lane}</dd>
            <dt>Status</dt>
            <dd>
              <Badge value={data.status} />
            </dd>
            <dt>Attempts</dt>
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
          <h3>Riwayat retry</h3>
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
          <span className="eyebrow">CLOUD LLM GATEWAY</span>
          <h2>LLM</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Rentang LLM" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
        <input aria-label="Task LLM" placeholder="Task" value={filters.task} onChange={(e) => setFilters({...filters, task:e.target.value})} />
        <select aria-label="Status LLM" value={filters.status} onChange={(e) => setFilters({...filters, status:e.target.value})}><option value="">Semua status</option><option>SUCCEEDED</option><option>FAILED</option></select>
      </div>
      <div className="admin-metrics">
        <Metric label="Calls 24 jam" value={number(summary.calls)} />
        <Metric
          label="Success rate"
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
          label="Tokens"
          value={number(
            (summary.inputTokens || 0) + (summary.outputTokens || 0),
          )}
        />
        <Metric label="Cost" value={summary.cost ?? "—"} />
      </div>
      <article className="surface admin-panel">
        <div className="section-title">
          <h2>Task breakdown</h2>
        </div>
        <Table
          headers={["Task", "Calls", "Gagal", "P50", "P95", "Tokens", "Biaya"]}
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
              <td>{x.cost ?? "—"}</td>
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
          <span className="eyebrow">STRUCTURED EVENTS</span>
          <h2>Logs</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Jenis event" value={filters.type} onChange={(e) => setFilters({...filters, type:e.target.value})}><option value="">Semua event</option><option>JOB_RETRY</option><option>JOB_FAILED</option><option>LLM_FAILED</option><option>SOURCE_FAILED</option></select>
        <select aria-label="Severity" value={filters.severity} onChange={(e) => setFilters({...filters, severity:e.target.value})}><option value="">Semua severity</option><option>WARN</option><option>ERROR</option></select>
        <input aria-label="Komponen" placeholder="Komponen" value={filters.component} onChange={(e) => setFilters({...filters, component:e.target.value})} />
        <select aria-label="Rentang log" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
        <input aria-label="Reference ID" placeholder="Reference ID" value={filters.q} onChange={(e) => setFilters({...filters, q:e.target.value})} />
      </div>
      <Table
        headers={[
          "Waktu",
          "Severity",
          "Event",
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
          <span className="eyebrow">HOUSEHOLD</span>
          <h2>Households</h2>
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
  return (
    <aside
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
            <dt>Gmail</dt>
            <dd>{data.integrations.gmailConnected}</dd>
            <dt>Bank listeners</dt>
            <dd>{data.integrations.activeBankListeners}</dd>
            <dt>Telegram</dt>
            <dd>{data.integrations.telegramLinked}</dd>
            <dt>Primary salary</dt>
            <dd>
              {data.integrations.primarySalaryConfigured ? "Ya" : "Tidak"}
            </dd>
          </dl>
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
          <span className="eyebrow">IDENTITY</span>
          <h2>Users</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <Table
        headers={[
          "User",
          "Email",
          "Status",
          "Households",
          "Password",
          "Role",
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
  const [kind, setKind] = useState("platform"), [householdId, setHouseholdId] = useState(""), [filters, setFilters] = useState({ action: "", range: "24h" });
  const platform = kind === "platform" || !householdId;
  const path = platform ? "/api/v1/admin/audit/platform" : "/api/v1/admin/audit/household";
  const [data, refresh, more] = useAdminList(path, platform ? filters : {...filters, householdId}, setError);
  if (!data) return <Empty>Memuat audit…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">IMMUTABLE PLATFORM EVENTS</span>
          <h2>Audit</h2>
        </div>
        <button className="secondary" onClick={refresh}>
          Perbarui
        </button>
      </div>
      <div className="admin-filters">
        <select aria-label="Jenis audit" value={kind} onChange={(e) => setKind(e.target.value)}><option value="platform">Platform</option><option value="household">Household</option></select>
        {kind === "household" && <input aria-label="ID household" placeholder="ID household" value={householdId} onChange={(e) => setHouseholdId(e.target.value)} />}
        <input aria-label="Action audit" placeholder="Action" value={filters.action} onChange={(e) => setFilters({...filters, action:e.target.value})} />
        <select aria-label="Rentang audit" value={filters.range} onChange={(e) => setFilters({...filters, range:e.target.value})}><option value="1h">1 jam</option><option value="24h">24 jam</option><option value="7d">7 hari</option><option value="30d">30 hari</option></select>
      </div>
      {kind === "household" && !householdId ? <Empty>Masukkan ID household untuk melihat audit scoped.</Empty> : <><Table headers={["Waktu", "Action", "Actor", "Entity", "Ringkasan"]}>
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
              <Empty>Belum ada platform audit.</Empty>
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
