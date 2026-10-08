"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";

// Styled replacements for window.confirm / window.prompt. The native <dialog>
// gives modal focus handling and Escape for free; the promise resolves to
// true/false for confirm and to { name: value } or null for prompt.
export default function useDialogs() {
  const [request, setRequest] = useState(null);
  const resolver = useRef(null);

  const ask = useCallback(options => new Promise(resolve => {
    resolver.current?.(options.kind === "confirm" ? false : null);
    resolver.current = resolve;
    setRequest(options);
  }), []);
  const settle = useCallback(value => {
    const resolve = resolver.current;
    resolver.current = null;
    setRequest(null);
    resolve?.(value);
  }, []);
  const confirm = useCallback((message, { confirmLabel = "Lanjutkan", danger = false } = {}) => ask({ kind: "confirm", message, confirmLabel, danger }), [ask]);
  const prompt = useCallback((title, fields, { confirmLabel = "Simpan" } = {}) => ask({ kind: "prompt", title, fields, confirmLabel }), [ask]);

  return { confirm, prompt, dialogs: request ? <DialogView request={request} onSettle={settle}/> : null };
}

function DialogView({ request, onSettle }) {
  const ref = useRef(null);
  const titleId = useId();
  const isConfirm = request.kind === "confirm";
  useEffect(() => {
    const element = ref.current;
    if (element && !element.open) element.showModal();
    return () => { if (element?.open) element.close(); };
  }, []);

  function submit(event) {
    event.preventDefault();
    if (isConfirm) { onSettle(true); return; }
    const form = new FormData(event.currentTarget);
    onSettle(Object.fromEntries(request.fields.map(field => [field.name, String(form.get(field.name) ?? "")])));
  }

  return <dialog ref={ref} aria-labelledby={titleId} onCancel={event => { event.preventDefault(); onSettle(isConfirm ? false : null); }}>
    <form onSubmit={submit}>
      {isConfirm
        ? <h2 id={titleId}>{request.message}</h2>
        : <><h2 id={titleId}>{request.title}</h2>{request.fields.map((field, index) => <label key={field.name}>{field.label}{field.multiline
          ? <textarea name={field.name} rows={5} defaultValue={field.defaultValue || ""} required={field.required} autoFocus={index === 0}/>
          : <input name={field.name} defaultValue={field.defaultValue || ""} required={field.required} autoFocus={index === 0}/>}</label>)}</>}
      <div className="dialog-actions">
        <button type="button" className="secondary" onClick={() => onSettle(isConfirm ? false : null)}>Batal</button>
        <button type="submit" className={request.danger ? "danger" : undefined} autoFocus={isConfirm && !request.danger}>{request.confirmLabel}</button>
      </div>
    </form>
  </dialog>;
}
