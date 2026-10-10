"use client";

import { useState } from "react";
import { time, number, percent, ms, Badge, Metric, Empty, useLoad, useAdminList, Table } from "./shared";

export default function Reviews({ setError }) {
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
  if (!summary || !breakdown) return <Empty>Memuat tinjauan…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">KESEHATAN REVIEW UNIVERSAL</span>
          <h2>Tinjauan</h2>
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
        <Metric label="Tinjauan terbuka" value={number(summary.openReviews)} />
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
          note={
            summary.legacyUnmeasuredReviews
              ? `Target 100% · ${number(summary.legacyUnmeasuredReviews)} legacy tak terukur`
              : "Target 100%"
          }
        />
        <Metric
          label="Dialihkan ke web"
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
          <h2>Per jenis tinjauan</h2>
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
            "Ke web",
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
