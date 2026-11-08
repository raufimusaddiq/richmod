"use client";

import { Badge, Pending, useLoad, Table } from "./shared";
import { useBusyMap } from "../lib/useLoadState";
import useDialogs from "../components/useDialogs";

// Server error codes from PATCH /api/v1/admin/users/{id}.
const userErrors = {
  ADMIN_SELF_LOCKOUT: "Kamu tidak bisa menonaktifkan akunmu sendiri atau mencabut Super Admin dari dirimu sendiri.",
  FINAL_SUPER_ADMIN_REQUIRED: "Harus tersisa minimal satu Super Admin aktif.",
  USER_NOT_FOUND: "Pengguna tidak ditemukan. Muat ulang daftar.",
  ADMIN_UPDATE_FAILED: "Perubahan gagal disimpan. Coba lagi.",
  SUPER_ADMIN_REQUIRED: "Hanya Super Admin yang dapat mengubah pengguna.",
};

export default function Users({ setError }) {
  const { confirm, dialogs } = useDialogs();
  const [users, refresh, state] = useLoad("/api/v1/admin/users", setError);
  const { busy, run } = useBusyMap();
  // One busy flag per user so a second click cannot race the first PATCH.
  const mutate = (user, patch, label) => run(user.id, async () => {
    setError("");
    if (!(await confirm(`${label} ${user.email}?`, { confirmLabel: label }))) return;
    try {
      const response = await fetch(`/api/v1/admin/users/${user.id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
      if (!response.ok) {
        const body = await response.json().catch(() => ({}));
        throw new Error(userErrors[body.error] || "Perubahan ditolak. Periksa invariant administrator.");
      }
      await refresh();
    } catch (err) {
      setError(err.message || "Koneksi terputus. Perubahan belum tersimpan.");
    }
  });
  if (!users) return <Pending state={state} retry={refresh}>Memuat users…</Pending>;
  return (
    <section className="admin-stack">
      {dialogs}
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
                disabled={Boolean(busy[user.id])}
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
                disabled={Boolean(busy[user.id])}
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
