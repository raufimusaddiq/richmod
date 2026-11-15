"use client";

import { useState } from "react";
import { displayDate } from "../lib/format";

// A native date input plus the chosen date spelled out ("15 Nov 2026"), so the
// browser's own mm/dd/yyyy vs dd/mm/yyyy ordering is never the only cue.
// Works uncontrolled (defaultValue) and controlled (value + onChange).
export default function DateInput({ onChange, ...props }) {
  const [own, setOwn] = useState(props.defaultValue ?? "");
  const value = props.value !== undefined ? props.value : own;
  const day = String(value || "").slice(0, 10);
  const label = /^\d{4}-\d{2}-\d{2}$/.test(day) ? displayDate(day) : "";
  return <>
    <input {...props} onChange={event => { setOwn(event.target.value); onChange?.(event); }}/>
    <small className="date-hint" aria-live="polite">{label}</small>
  </>;
}
