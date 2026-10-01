"use client";

import { useState } from "react";
import { time, Badge, Empty, useDrawerA11y, useLoad, useAdminList, Table } from "./shared";

export default function Jobs({ setError }) {
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
