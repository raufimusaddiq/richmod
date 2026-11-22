import { dateTime } from "../lib/format";
import { emailDeliveryReasonLabel, emailDeliveryStatusLabel, label } from "../lib/labels";

export default function EmailDeliveries({ state, retry, loadMore }) {
  if (state.status === "error") return <p role="alert">Email masuk belum dapat dimuat. <button type="button" className="secondary" onClick={retry}>Coba lagi</button></p>;
  if (state.status === "idle" || state.status === "loading") return <p className="muted">Memuat…</p>;
  if (!state.items.length) return <div className="empty-state"><span aria-hidden="true">✉</span><h2>Belum ada email masuk</h2><p>Email yang diteruskan ke alamat Richmod akan tercatat di sini.</p></div>;
  return <><ul className="delivery-list">{state.items.map((item, index) => <li key={`${item.receivedAt}-${index}`}>
    <div className="delivery-main"><b>{label(emailDeliveryStatusLabel, item.status)}</b><small>{item.sender || "Pengirim tidak diketahui"} · {dateTime(item.receivedAt)}</small></div>
    {item.reason && <span className="delivery-reason">{label(emailDeliveryReasonLabel, item.reason, item.reason)}</span>}
  </li>)}</ul>{state.next && <div className="review-actions"><p className="muted" role="status">Menampilkan {state.items.length} email terbaru.{state.moreError ? " Email yang lebih lama belum dapat dimuat." : ""}</p><button type="button" className="secondary" disabled={state.more} onClick={loadMore}>{state.more ? "Memuat…" : "Muat email lebih lama"}</button></div>}</>;
}
