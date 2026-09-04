"use client";

import { useCallback, useEffect, useState } from "react";
import AppShell from "../components/AppShell";
import useAuth from "../components/useAuth";
import { ErrorNotice, Skeleton, Toast } from "../components/Feedback";

export default function ActionsPage() {
  const user = useAuth();
  const [items, setItems] = useState([]), [loading, setLoading] = useState(true), [error, setError] = useState(""), [toast, setToast] = useState(""), [working, setWorking] = useState("");
  const load = useCallback(async () => { setLoading(true); try { const response = await fetch("/api/v1/integration-actions"); if (!response.ok) throw new Error(); const value = await response.json(); setItems(Array.isArray(value) ? value : []); setError(""); } catch { setError("Tindakan integrasi belum dapat dimuat."); } finally { setLoading(false); } }, []);
  useEffect(() => { if (user) load(); }, [user, load]);
  async function resolve(id) { setWorking(id); const response = await fetch(`/api/v1/integration-actions/${id}/resolve`, { method: "POST" }); if (response.ok) { setItems(items.filter(item => item.id !== id)); setToast("Tindakan ditandai selesai."); } else setError("Tindakan belum dapat ditandai selesai."); setWorking(""); }
  if (user === null) return <main className="loading"><span>Memuat Richmod…</span></main>;
  return <AppShell user={user} eyebrow="INTEGRASI" title="Tindakan integrasi" actions={<span className="header-meta">{items.length} menunggu</span>}><ErrorNotice message={error} retry={load}/>{loading ? <Skeleton cards={2}/> : <section className="action-list">{items.map(item => <article key={item.id} className="surface action-card"><div><span className="eyebrow">{item.integrationType === "EMAIL_FORWARDING" ? "EMAIL" : item.integrationType}</span><h2>{item.title}</h2><p>{item.description}</p><small>Diterima {new Date(item.createdAt).toLocaleString("id-ID", { dateStyle: "medium", timeStyle: "short" })}</small>{item.actionCode && <p className="action-code">Kode konfirmasi: <code>{item.actionCode}</code></p>}</div><div className="action-buttons">{item.actionUrl && <a className="button" href={item.actionUrl} target="_blank" rel="noopener noreferrer">Verifikasi penerusan</a>}<button className="secondary" disabled={working === item.id} onClick={() => resolve(item.id)}>Tandai selesai</button></div></article>)}{!items.length && <div className="empty-state"><span>✓</span><h2>Tidak ada tindakan tertunda</h2><p>Richmod akan menampilkan kebutuhan setup integrasi di sini.</p></div>}</section>}<Toast message={toast} onClose={() => setToast("")}/></AppShell>;
}
