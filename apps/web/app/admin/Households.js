"use client";

import { useState } from "react";
import { time, Badge, Empty, useDrawerA11y, useLoad, Table } from "./shared";

// Server error codes from POST /api/v1/admin/households and owner-invite.
const provisionErrors = {
  USER_ALREADY_EXISTS: "Email ini sudah dipakai pengguna lain. Rumah tangga baru butuh email pemilik baru.",
  TELEGRAM_ID_ALREADY_LINKED: "Telegram user ID ini sudah terikat ke pengguna lain (termasuk anggota yang sudah dihapus) dan tidak bisa dipakai ulang.",
  INVALID_HOUSEHOLD_REQUEST: "Periksa nama rumah tangga, nama pemilik, email, dan Telegram user ID.",
  OWNER_ALREADY_ENROLLED: "Pemilik sudah mengaktifkan akunnya; undangan baru tidak diperlukan.",
  HOUSEHOLD_OWNER_NOT_FOUND: "Pemilik aktif rumah tangga ini tidak ditemukan.",
  HOUSEHOLD_CREATE_FAILED: "Rumah tangga belum dapat dibuat. Tidak ada yang tersimpan; coba lagi.",
  OWNER_INVITE_FAILED: "Undangan pemilik belum dapat dibuat. Coba lagi.",
  ADMIN_AUDIT_FAILED: "Audit platform gagal dicatat, jadi perubahan dibatalkan. Coba lagi.",
  SUPER_ADMIN_REQUIRED: "Hanya Super Admin yang dapat membuat rumah tangga.",
};

export async function postProvision(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: payload ? JSON.stringify(payload) : undefined,
  });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(provisionErrors[body.error] || "Permintaan ditolak server.");
  return body;
}

// The owner invite is shown once: only its hash is stored, so a lost link is
// replaced with "Buat ulang undangan pemilik", never recovered.
function InviteLink({ invite, title }) {
  const link = typeof window === "undefined" ? invite.link : window.location.origin + invite.link;
  const [copied, setCopied] = useState("");
  // The link is shown once, so say plainly whether it reached the clipboard;
  // without a secure context the browser has no clipboard API at all.
  async function copy() {
    try {
      if (!navigator.clipboard) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(link);
      setCopied("Tautan tersalin.");
    } catch {
      setCopied("Tidak bisa menyalin otomatis. Pilih tautan di kolom lalu salin manual.");
    }
  }
  return (
    <section className="panel" aria-live="polite">
      <div>
        <span className="eyebrow">UNDANGAN PEMILIK</span>
        <h2>{title}</h2>
        <p>Kirim tautan ini langsung kepada pemilik. Sekali pakai, berlaku sampai {time(invite.expiresAt)}, dan hanya ditampilkan sekarang.</p>
      </div>
      <div className="invite-actions">
        <input aria-label="Tautan undangan pemilik" readOnly value={link} onFocus={(event) => event.target.select()} />
        <button className="secondary" onClick={copy}>Salin tautan</button>
      </div>
      {copied && <p className="notice" role="status">{copied}</p>}
    </section>
  );
}

function CreateHousehold({ onCreated, setError }) {
  const [busy, setBusy] = useState(false);
  async function submit(event) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const telegram = String(form.get("telegramUserId") || "").trim();
    if (telegram && !/^[1-9][0-9]{0,17}$/.test(telegram)) {
      setError("Telegram user ID harus berupa angka positif.");
      return;
    }
    setBusy(true);
    try {
      const created = await postProvision("/api/v1/admin/households", {
        householdName: form.get("householdName"),
        ownerName: form.get("ownerName"),
        ownerEmail: form.get("ownerEmail"),
        ...(telegram ? { telegramUserId: Number(telegram) } : {}),
      });
      event.target.reset();
      onCreated(created);
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel">
      <div className="panel-title">
        <h2>Buat rumah tangga</h2>
        <span>Pemilik baru diundang lewat tautan</span>
      </div>
      <form className="member-form" onSubmit={submit}>
        <input aria-label="Nama rumah tangga" name="householdName" placeholder="Nama rumah tangga" maxLength="120" required />
        <input aria-label="Nama pemilik" name="ownerName" placeholder="Nama pemilik" maxLength="120" required />
        <input aria-label="Email pemilik" name="ownerEmail" type="email" placeholder="Email pemilik" maxLength="254" required />
        <input aria-label="Telegram user ID pemilik (opsional)" name="telegramUserId" inputMode="numeric" placeholder="Telegram user ID (opsional)" />
        <button disabled={busy}>{busy ? "Membuat…" : "Buat rumah tangga"}</button>
      </form>
    </section>
  );
}

export default function Households({ setError }) {
  const [data, refresh] = useLoad("/api/v1/admin/households", setError),
    [selected, setSelected] = useState(null),
    [creating, setCreating] = useState(false),
    [created, setCreated] = useState(null);
  if (!data) return <Empty>Memuat household…</Empty>;
  return (
    <section className="admin-stack">
      <div className="admin-section-head">
        <div>
          <span className="eyebrow">RUMAH TANGGA</span>
          <h2>Rumah tangga</h2>
        </div>
        <div className="admin-actions">
          <button onClick={() => setCreating((open) => !open)} aria-expanded={creating}>
            {creating ? "Tutup formulir" : "Buat rumah tangga"}
          </button>
          <button className="secondary" onClick={refresh}>
            Perbarui
          </button>
        </div>
      </div>
      {creating && (
        <CreateHousehold
          setError={setError}
          onCreated={(result) => {
            setCreated(result);
            setCreating(false);
            refresh();
          }}
        />
      )}
      {created && <InviteLink invite={created.invite} title={`${created.household.name} dibuat untuk ${created.owner.email}`} />}
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
  const [data, reload] = useLoad(`/api/v1/admin/households/${id}/overview`, setError);
  const [ownerInvite, setOwnerInvite] = useState(null);
  const [reissuing, setReissuing] = useState(false);
  const drawer = useDrawerA11y(close);
  const pendingOwner = data?.memberItems?.find((x) => x.role === "OWNER" && x.active && !x.enrolled);
  // Each reissue revokes the previous link, so a double click must not
  // silently invalidate the link just shown.
  async function reissue() {
    if (reissuing) return;
    setReissuing(true);
    try {
      setOwnerInvite(await postProvision(`/api/v1/admin/households/${id}/owner-invite`));
      reload();
    } catch (err) {
      setError(err.message);
    } finally {
      setReissuing(false);
    }
  }
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
          {data.memberItems?.length ? <Table headers={["Nama", "Email", "Role", "Status", "Akun"]}>{data.memberItems.map((x) => <tr key={x.id}><td>{x.displayName}</td><td>{x.email}</td><td>{x.role}</td><td><Badge value={x.active ? "ACTIVE" : "INACTIVE"} /></td><td>{x.enrolled ? "Aktif" : "Belum diaktifkan"}</td></tr>)}</Table> : <Empty>Tidak ada anggota.</Empty>}
          {pendingOwner && (
            <div className="admin-actions">
              <button className="secondary" onClick={reissue} disabled={reissuing}>{reissuing ? "Membuat undangan…" : "Buat ulang undangan pemilik"}</button>
            </div>
          )}
          {ownerInvite && <InviteLink invite={ownerInvite} title={`Undangan baru untuk ${pendingOwner?.email || "pemilik"}`} />}
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
