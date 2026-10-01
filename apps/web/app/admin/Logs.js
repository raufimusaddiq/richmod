"use client";

import { useState } from "react";
import { time, Badge, Empty, useAdminList, Table } from "./shared";

export default function Logs({ setError }) {
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
