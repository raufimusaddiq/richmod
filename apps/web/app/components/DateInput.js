"use client";

import { useState } from "react";
import { displayDate } from "../lib/format";

export default function DateInput({ onChange, ...props }) {
  const [own, setOwn] = useState(props.defaultValue ?? "");
  const value = props.value !== undefined ? props.value : own;
  const day = String(value || "").slice(0, 10);
  const label = /^\d{4}-\d{2}-\d{2}$/.test(day) ? displayDate(day) : "";
  return <>
    <input {...props} onChange={event => { setOwn(event.target.value); onChange?.(event); }}/>
    <small className="date-hint" data-date={label} aria-hidden="true"/>
  </>;
}
