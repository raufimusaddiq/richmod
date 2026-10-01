"use client";

import { Badge, Empty, useLoad, Table } from "./shared";
import useDialogs from "../components/useDialogs";

export default function Users({ setError }) {
  const { confirm, dialogs } = useDialogs();
  const [users, refresh] = useLoad("/api/v1/admin/users", setError);
  const mutate = async (user, patch, label) => {
    if (!(await confirm(`${label} ${user.email}?`, { confirmLabel: label }))) return;
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
