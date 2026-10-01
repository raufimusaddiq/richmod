import { amountLabel, changeWidth, measuredLabel, qualityCopy, ratioLabel, reviewSteps, signedMoney, transactionHref } from "../lib/cycleReview";
import { dateTime, money, typeLabel } from "../lib/format";
import { SectionTitle, Metric } from "./shared";

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

export function CyclePosition({ facts }) {
  const active = facts.period.state === "ACTIVE"; // income lands on day 1, so net cashflow is not the headline until the cycle closes
  return <section id="position" className="review-section cycle-position" aria-labelledby="position-title">
    <SectionTitle id="position-title" about="posisi siklus" title="Posisi siklus" description={active ? "Hanya transaksi terkonfirmasi. Pengeluaran bersih setelah refund; transfer bukan pengeluaran. Selama siklus berjalan, arus kas bersih belum final karena pengeluaran masih bertambah." : "Hanya transaksi terkonfirmasi. Pengeluaran bersih setelah refund; transfer bukan pengeluaran."}/>
    <dl className="cycle-outcome">
      {active
        ? <div className="cycle-net"><dt>Pengeluaran bersih sejauh ini</dt><dd>{money(facts.cashflow.expense)}</dd></div>
        : <div className="cycle-net"><dt>Arus kas bersih</dt><dd>{money(facts.cashflow.netCashflow)}</dd></div>}
      <Metric label="Pemasukan" value={money(facts.cashflow.income)}/>
      {active ? <Metric label="Arus kas bersih sejauh ini" value={money(facts.cashflow.netCashflow)}/> : <Metric label="Pengeluaran bersih" value={money(facts.cashflow.expense)}/>}
      <Metric label="Tabungan dialokasikan" value={money(facts.cashflow.savingsAllocated)}/>
      <Metric label="Surplus belum dialokasikan" value={money(facts.cashflow.unallocatedSurplus)}/>
    </dl>
    <div className="cycle-footnote"><span>Refund <strong>{money(facts.cashflow.refund)}</strong></span><details className="review-explainer"><summary>Tentang surplus</summary><p>Surplus belum dialokasikan adalah arus kas bersih dikurangi transfer tabungan terkonfirmasi, bukan transaksi tambahan.</p></details></div>
  </section>;
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
    <div className="change-ranking-head" aria-hidden="true"><span>Kategori</span><span>{elapsed ? "Siklus berjalan" : "Siklus ini"}</span><span>Selisih vs siklus sebelumnya</span></div>
    <ul className="change-ranking" aria-label="Perubahan kategori">
      {items.map(item => <li key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
        <button className="change-button" type="button" aria-controls="category-drivers" aria-pressed={selected === (item.id || "uncategorized")} onClick={() => select(item.id || "uncategorized")}>{item.name}</button>
        <span><span className="visually-hidden">Siklus ini: </span>{money(item.amount)}</span>
        <span><span className="visually-hidden">Selisih vs siklus sebelumnya (penuh): </span>{signedMoney(item.deltaVsPreviousFullCycle)}<small> · {ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></span>
        <span className="change-track" aria-hidden="true"><i data-negative={String(item.deltaVsPreviousFullCycle).startsWith("-") || undefined} style={{ width: changeWidth(item, items, "deltaVsPreviousFullCycle") }}/></span>
      </li>)}
    </ul>
    <small>Batang menunjukkan besar selisih relatif, bukan persentase kenaikan.</small>
    <details className="review-daily"><summary>Perbandingan lengkap · median, persentase & kontribusi</summary>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Perbandingan kategori lengkap, geser untuk semua kolom"><table className="changes-table">
    <caption>Perubahan kategori. Persentase tanpa pembanding positif ditampilkan sebagai —. Sebelumnya · penuh: {measuredLabel(comparison.previousFullCycle)}.</caption>
    <thead><tr><th scope="col">Kategori / bukti</th><th scope="col">Siklus ini</th><th scope="col">{elapsed ? "Sebelumnya · hari yang sama" : "Sebelumnya · penuh"}</th><th scope="col">{elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus"}</th><th scope="col">{deltaLabel}</th><th scope="col">{elapsed ? "Selisih vs median · hari yang sama" : "Selisih vs median"}</th><th scope="col">{elapsed ? "Kontribusi ke selisih hari yang sama" : "Kontribusi ke selisih total"}</th>{elapsed && <><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th></>}</tr></thead>
    <tbody>{items.map(item => <tr key={item.id || "uncategorized"} data-selected={selected === (item.id || "uncategorized")}>
      <th scope="row">{item.name}</th>
      <td>{money(item.amount)}</td><td>{amountLabel(item.previous)}</td><td>{amountLabel(item.median3)}</td>
      <td>{signedMoney(item.deltaVsPrevious)}<small>{ratioLabel(item.relativeDeltaVsPrevious)}</small></td>
      <td>{signedMoney(item.deltaVsMedian3)}<small>{ratioLabel(item.relativeDeltaVsMedian3)}</small></td>
      <td>{ratioLabel(item.contributionToExpenseChange)}</td>
      {elapsed && <><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td></>}
    </tr>)}</tbody>
  </table></div></details></>;
}

export function MerchantTable({ items, period, categoryId }) {
  if (!items.length) return <p className="empty compact">Belum ada merchant pendukung.</p>;
  const elapsed = period.state === "ACTIVE";
  return <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Merchant pendukung, geser untuk semua kolom"><table><caption>Merchant pendukung (maks. 10). Nilai bersih setelah refund.</caption><thead><tr><th scope="col">Merchant</th><th scope="col">Siklus ini</th><th scope="col">Sebelumnya · penuh</th><th scope="col">Selisih vs siklus sebelumnya (penuh)</th>{elapsed && <><th scope="col">Sebelumnya · hari yang sama</th><th scope="col">Selisih vs hari yang sama</th></>}<th scope="col">{elapsed ? "Median 3 siklus · hari yang sama" : "Median 3 siklus"}</th></tr></thead><tbody>{items.map((item, index) => <tr key={`${item.id}:${index}`}><th scope="row">{item.id ? <a href={transactionHref(period, { merchantId: item.id, categoryId })}>{item.name}</a> : item.name}</th><td>{money(item.amount)}</td><td>{amountLabel(item.previousFullCycle)}</td><td>{signedMoney(item.deltaVsPreviousFullCycle)}<small>{ratioLabel(item.relativeDeltaVsPreviousFullCycle)}</small></td>{elapsed && <><td>{amountLabel(item.previous)}</td><td>{signedMoney(item.deltaVsPrevious)}</td></>}<td>{amountLabel(item.median3)}</td></tr>)}</tbody></table></div>;
}

export function CategoryDrivers({ item, period }) {
  return <div className="category-evidence">
    <h3>{item.name}</h3>
    {!period.reviewStep && <a className="evidence-back" href="#ledger">Kembali ke ringkasan siklus</a>}
    <dl className="review-context"><Metric label="Porsi pengeluaran siklus" value={ratioLabel(item.shareOfExpense)}/><Metric label="Jumlah transaksi" value={item.count}/></dl>
    <MerchantTable items={item.merchants} period={period} categoryId={item.id || "uncategorized"}/>
    <div className="review-table-wrap" tabIndex={0} role="region" aria-label="Transaksi pendukung"><table><caption>Transaksi pendukung terbesar (maks. 10). Refund ditandai terpisah.</caption><thead><tr><th scope="col">Merchant / tanggal</th><th scope="col">Jenis</th><th scope="col">Jumlah</th><th scope="col">Bukti</th></tr></thead><tbody>{item.transactions.map(transaction => <tr key={transaction.id}><th scope="row">{transaction.merchant}<small>{dateTime(transaction.transactionAt)}</small></th><td>{typeLabel[transaction.type]}</td><td>{money(transaction.amount)}</td><td><a href={transactionHref(period, { categoryId: item.id || "uncategorized", id: transaction.id })}>Buka transaksi</a></td></tr>)}</tbody></table></div>
    {!item.transactions.length && <p className="empty compact">Tidak ada transaksi terkonfirmasi kategori ini pada siklus terpilih.</p>}
    <a href={transactionHref(period, { categoryId: item.id || "uncategorized" })}>Lihat transaksi {item.name} dalam periode ini</a>
  </div>;
}

export function SavingsWealth({ facts }) {
  const { wealth, cashflow } = facts;
  return <section id="savings-wealth" className="review-section" aria-labelledby="savings-title">
    <SectionTitle id="savings-title" about="tabungan dan kekayaan" title="Tabungan & kekayaan" description="Alokasi tabungan adalah transfer terkonfirmasi; kekayaan adalah pengamatan saldo, bukan transaksi."/>
    <div className="review-split">
      <div><h3>Ke mana surplus dialokasikan?</h3><dl className="review-context">
        <Metric label="Tabungan dialokasikan" value={money(cashflow.savingsAllocated)}/><Metric label="Belum dialokasikan" value={money(cashflow.unallocatedSurplus)}/>
      </dl>
      <dl className="review-destinations">{facts.savingsDestinations.map(item => <Metric key={item.id || item.name} label={item.name} value={money(item.amount)}/>)}</dl>
      {!facts.savingsDestinations.length && <p>Belum ada alokasi tabungan terkonfirmasi.</p>}
      </div>
      <div><h3>Pergerakan kekayaan</h3><dl className="review-context">
        <Metric label="Perubahan kekayaan bersih" value={signedMoney(wealth.netWorthChange)}/>
      </dl>
      {wealth.netWorthChange == null && <p>Belum dapat direkonsiliasi.</p>}
      <details className="review-daily"><summary>Rincian saldo & rekonsiliasi</summary><dl className="review-context">
        <Metric label="Kekayaan bersih sebelumnya" value={amountLabel(wealth.previous?.netWorth)}/><Metric label="Kekayaan bersih terbaru" value={amountLabel(wealth.current?.netWorth)}/>
        <Metric label="Kontribusi arus kas terkonfirmasi" value={amountLabel(wealth.confirmedCashflow)}/>
        <Metric label="Valuasi & perubahan lain" value={signedMoney(wealth.valuationAndOtherChange)}/>
      </dl>
      <details className="review-explainer"><summary>Tentang rekonsiliasi</summary><p>Rekonsiliasi mengikuti selang waktu pengamatan, bukan saldo akhir siklus yang diperkirakan. Selisih lainnya bukan laba investasi atau transfer tabungan.</p></details>
      {[["Sebelumnya", wealth.previous], ["Terbaru", wealth.current]].map(([label, snapshot]) => <p className="snapshot-context" key={label}>{label}: {snapshot ? <><a href={`/wealth?snapshotId=${encodeURIComponent(snapshot.id)}`}>{dateTime(snapshot.observedAt)}</a> · usia {snapshot.ageDays} hari pada batas pengukuran</> : "belum tersedia"}</p>)}
      {wealth.netWorthChange == null && <p>Pergerakan belum dapat direkonsiliasi. Periksa catatan dan kelengkapan akun.</p>}
      </details>
      <a href="/wealth">Buka detail Kekayaan</a></div>
    </div>
  </section>;
}

export function QualitySection({ facts }) {
  return <section id="quality" className="review-section" aria-labelledby="quality-title">
    <SectionTitle id="quality-title" about="kelengkapan data" title="Kelengkapan data & tindak lanjut" description="Hal yang belum lengkap tetap terlihat. Tidak ada skor kepercayaan dari model."/>
    {!facts.dataQuality.length ? <p>Tidak ada kendala yang tercatat untuk periode ini.</p> : <ul className="review-quality">{facts.dataQuality.map(blocker => {
      const [label, action] = qualityCopy[blocker.kind] || ["Data tinjauan perlu dilengkapi", "Buka Inbox"];
      const counted = ["OPEN_REVIEWS", "UNCATEGORIZED_EXPENSE", "PROCESSING_INCOMPLETE"].includes(blocker.kind);
      return <li key={blocker.kind}><div><strong>{counted ? `${blocker.count} ` : ""}{label}</strong>{blocker.amount != null && <p>{money(blocker.amount)} terkonfirmasi.</p>}</div><a href={blocker.kind === "UNCATEGORIZED_EXPENSE" ? transactionHref(facts.period, { type: "EXPENSE", categoryId: "uncategorized" }) : ["/inbox", "/wealth", "/settings"].includes(blocker.action) ? blocker.action : "/inbox"}>{action}</a></li>;
    })}</ul>}
  </section>;
}
