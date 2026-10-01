"use client";

import { useState } from "react";
import { time, Badge, Empty, useDrawerA11y, useLoad, Table } from "./shared";

export default function Households({ setError }) {
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
          "Keluarga",
          "Anggota",
          "Transaksi",
          "Tinjauan",
          "Aktivitas terakhir",
          "Dibuat",
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
