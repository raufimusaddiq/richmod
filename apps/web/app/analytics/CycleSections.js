import { amountLabel, measuredLabel, ratioLabel, reviewSteps, signedMoney, transactionHref } from "../lib/cycleReview";
import { money } from "../lib/format";
import { Metric } from "./shared";

export function MeetingNav({ step, selection, navigate }) {
  const index = reviewSteps.findIndex(([id]) => id === step);
  return <div className="meeting-controls">
    <p role="status">{index + 1} / {reviewSteps.length} · {reviewSteps[index][1]} <small>Sesi tinjauan; keputusan disimpan terpisah.</small></p>
    <nav aria-label="Langkah tinjauan siklus"><ol>{reviewSteps.map(([id, label], i) => <li key={id}><button type="button" className={id === step ? "active" : "secondary"} aria-current={id === step ? "step" : undefined} onClick={() => navigate({ step: id })}>{i + 1}. {label}</button></li>)}</ol></nav>
    <div className="meeting-actions">
      <button type="button" className="secondary" disabled={index === 0} onClick={() => navigate({ step: reviewSteps[index - 1][0] })}>Langkah sebelumnya</button>
      {index < reviewSteps.length - 1 && <button type="button" onClick={() => navigate({ step: reviewSteps[index + 1][0] })}>Langkah berikutnya</button>}
      <button type="button" className="secondary" onClick={() => navigate({ step: "" })}>{index === reviewSteps.length - 1 ? "Selesai meninjau" : "Tampilkan seluruh tinjauan"}</button>
    </div>
  </div>;
}

export function ComparisonContext({ comparison, period }) {
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const rows = [[elapsed ? "Siklus ini · berjalan" : "Siklus ini", comparison.expense.amount], ["Sebelumnya · penuh", comparison.expense.previousFullCycle], ...(elapsed ? [["Sebelumnya · hari yang sama", comparison.expense.previous]] : []), [elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus", comparison.expense.median3]];
  const maximum = Math.max(0, ...rows.map(([, value]) => Math.abs(Number(value))));
  return <div className="review-baseline">
    <div className="baseline-meta"><span>{measuredLabel(period)}{elapsed && " · berjalan"}</span><span>{comparison.previousFullCycle ? `Sebelumnya · penuh: ${measuredLabel(comparison.previousFullCycle)}` : "Belum ada siklus pembanding"}</span>{elapsed && <span>Hari yang sama, bukan siklus penuh: {measuredLabel(comparison.previous)}</span>}<span>{comparison.median3Available ? "Median 3 siklus tersedia" : `Median belum tersedia · ${comparison.eligibleCycles}/3 siklus`}</span></div>
    <dl className="comparison-bars" aria-label="Perbandingan pengeluaran bersih">
      {rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd><span aria-hidden="true" className="comparison-track"><i data-negative={String(value).startsWith("-") || undefined} style={{ width: maximum ? `${Math.abs(Number(value)) / maximum * 100}%` : "0%" }}/></span><strong>{amountLabel(value)}</strong></dd></div>)}
    </dl>
    <dl className="comparison-deltas">
      <Metric label="Selisih vs siklus sebelumnya (penuh)" value={`${signedMoney(comparison.expense.deltaVsPreviousFullCycle)} · ${ratioLabel(comparison.expense.relativeDeltaVsPreviousFullCycle)}`}/>
      {elapsed && <Metric label="Selisih vs hari yang sama" value={signedMoney(comparison.expense.deltaVsPrevious)}/>}
      <Metric label={elapsed ? "Selisih vs median 3 siklus · hari yang sama" : "Selisih vs median 3 siklus"} value={signedMoney(comparison.expense.deltaVsMedian3)}/>
    </dl>
  </div>;
}

export function ChangesTable({ items, comparison, selected, select }) {
  if (!items.length) return <p className="empty compact">Belum ada pengeluaran kategori pada periode ini atau pembandingnya.</p>;
  const elapsed = comparison.mode === "ELAPSED_DAYS";
  const deltaLabel = elapsed ? "Selisih vs hari yang sama" : "Selisih vs sebelumnya";
  return <>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"><table className="changes-table">
    <caption>Perubahan kategori. Persentase tanpa pembanding positif ditampilkan sebagai —. Sebelumnya · penuh: {measuredLabel(comparison.previousFullCycle)}.</caption>
    <thead><tr><th scope="col">Kategori / bukti</th><th scope="col">Siklus ini</th><th scope="col">{elapsed ? "Sebelumnya · hari yang sama" : "Sebelumnya · penuh"}</th><th scope="col">{elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus"}</th><th scope="col">{deltaLabel}</th><th scope="col">{elapsed ? "Selisih vs median · hari yang sama" : "Selisih vs median"}</th><th scope="col">{elapsed ? "Kontribusi ke selisih hari yang sama" : "Kontribusi ke selisih total"}</th>{elapsed && <><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th></>}</tr></thead>
    <tbody>{items.map(item => <tr key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
      <th scope="row"><button className="change-button" type="button" aria-haspopup="dialog" aria-controls="category-drawer" onClick={event => select(item.id || "uncategorized", event.currentTarget)}>{item.name}</button></th>
      <td>{money(item.amount)}</td><td>{amountLabel(item.previous)}</td><td>{amountLabel(item.median3)}</td>
      <td>{signedMoney(item.deltaVsPrevious)}<small>{ratioLabel(item.relativeDeltaVsPrevious)}</small></td>
      <td>{signedMoney(item.deltaVsMedian3)}<small>{ratioLabel(item.relativeDeltaVsMedian3)}</small></td>
      <td>{ratioLabel(item.contributionToExpenseChange)}</td>
      {elapsed && <><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td></>}
    </tr>)}</tbody>
  </table></div></>;
}

export function MerchantTable({ items, period, categoryId }) {
  if (!items.length) return <p className="empty compact">Belum ada merchant pendukung.</p>;
  const elapsed = period.state === "ACTIVE";
  return <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Merchant pendukung, geser untuk semua kolom"><table><caption>Merchant pendukung (maks. 10). Nilai bersih setelah refund.</caption><thead><tr><th scope="col">Merchant</th><th scope="col">Siklus ini</th><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th>{elapsed && <><th scope="col">Sebelumnya · hari yang sama</th><th scope="col">Selisih vs hari yang sama</th></>}<th scope="col">{elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus"}</th></tr></thead><tbody>{items.map((item, index) => <tr key={`${item.id}:${index}`}><th scope="row">{item.id ? <a href={transactionHref(period, { merchantId: item.id, categoryId })}>{item.name}</a> : item.name}</th><td>{money(item.amount)}</td><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td>{elapsed && <><td>{amountLabel(item.previous)}</td><td>{signedMoney(item.deltaVsPrevious)}</td></>}<td>{amountLabel(item.median3)}</td></tr>)}</tbody></table></div>;
}
