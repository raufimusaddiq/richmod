"use client";

import { useState } from "react";
import { time, Empty, useAdminList, Table } from "./shared";

export default function Audit({ setError }) {
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
