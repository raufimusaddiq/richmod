"use client";
import { useState } from "react";
import { dateTime, money } from "../../lib/format";
import { reviewLabel } from "./shared";

const PREVIEW = 5;
const outcomes = {
  CONFIRM_REVIEW: "Disimpan", TELEGRAM_CONFIRMED: "Disimpan", COMPLETE_BANK_FACTS: "Dilengkapi", TELEGRAM_DATE_SET: "Tanggal diisi",
  CLASSIFY_TRANSFER: "Transfer dikelompokkan", ALLOCATE_RETAINED_BALANCE: "Sisa dialokasikan", CONFIRM_EVIDENCE_DISPUTE: "Bukti digabung",
  IGNORE: "Diabaikan", TELEGRAM_REJECTED: "Diabaikan", LEAVE_UNALLOCATED: "Dibiarkan", NO_LONGER_APPLICABLE: "Tidak berlaku",
};
export function outcomeLabel(action) { return outcomes[action] || "Selesai"; }

// One quiet line per decision: what it was, how it ended, when; the reopen
// button only exists where a reopen is actually possible.
export default function ReviewHistory({ items, busy, reopen }) {
  const [all, setAll] = useState(false);
  const shown = all ? items : items.slice(0, PREVIEW);
  return <>
    <ul className="history-list">{shown.map(item => <li key={item.id}>
      <div className="history-main">
        <span className="history-title">{item.summary}</span>
        <span className="history-meta">{reviewLabel(item.reviewType)} · {outcomeLabel(item.resolutionAction)} · {dateTime(item.resolvedAt)}</span>
      </div>
      {item.amountIdr && <strong className="history-amount">{money(item.amountIdr)}</strong>}
      {item.reopenable && <button type="button" className="secondary compact" disabled={Boolean(busy[item.id])} onClick={() => reopen(item.id)}>Buka lagi</button>}
    </li>)}</ul>
    {items.length > PREVIEW && <button type="button" className="link-button" onClick={() => setAll(value => !value)}>{all ? "Ringkas" : `Tampilkan semua (${items.length})`}</button>}
  </>;
}
