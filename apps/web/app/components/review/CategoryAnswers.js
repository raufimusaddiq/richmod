"use client";
import { useState } from "react";
import { categoryAnswers, rememberLabel } from "../../lib/reviewInbox";

export default function CategoryAnswers({ item, categories, disabled, action }) {
  const [more, setMore] = useState(false);
  const [remember, setRemember] = useState(false);
  const { answers, others } = categoryAnswers(item, categories);
  const place = rememberLabel(item);
  const canRemember = item.type === "EXPENSE" && Boolean(place);
  const choose = categoryId => action(item.id, "confirm", { categoryId, ...(canRemember && remember ? { rememberMerchant: true, merchantName: place } : {}) });
  return <div className="review-ask">
    <p className="review-question" id={`ask-${item.id}`}>Masuk kategori apa?</p>
    <div className="answer-row" role="group" aria-labelledby={`ask-${item.id}`}>
      {answers.map((answer, index) => <button type="button" key={answer.id} className={index === 0 ? "answer top" : "answer"} disabled={disabled} onClick={() => choose(answer.id)}>{answer.name}{answer.often && <small>sering dipakai</small>}</button>)}
      {others.length > 0 && <button type="button" className="answer more" aria-expanded={more} aria-controls={`more-${item.id}`} onClick={() => setMore(value => !value)}>Kategori lain…</button>}
    </div>
    {more && <div className="answer-row" id={`more-${item.id}`} role="group" aria-label="Kategori lain">{others.map(other => <button type="button" key={other.id} className="answer" disabled={disabled} onClick={() => choose(other.id)}>{other.name}</button>)}</div>}
    {canRemember && <label className="check review-check"><input type="checkbox" checked={remember} onChange={event => setRemember(event.target.checked)}/> Ingat untuk {place}</label>}
  </div>;
}
