"use client";

import { useEffect, useRef } from "react";

const focusable = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

// Modal drawer behaviour the markup alone cannot give: move focus in, close on
// Escape, keep Tab inside the drawer, and return focus to the opener on close.
// Attach the returned ref to the `aside[role="dialog"]` element.
export default function useDrawerA11y(open, close) {
  const ref = useRef(null);
  const closeRef = useRef(close);
  closeRef.current = close;

  useEffect(() => {
    if (!open) return undefined;
    const opener = document.activeElement;
    const drawer = ref.current;
    drawer?.focus();
    const onKeyDown = event => {
      if (event.key === "Escape") { closeRef.current(); return; }
      if (event.key !== "Tab" || !drawer) return;
      const items = [...drawer.querySelectorAll(focusable)];
      if (!items.length) { event.preventDefault(); return; }
      const first = items[0], last = items[items.length - 1];
      if (event.shiftKey && (document.activeElement === first || document.activeElement === drawer)) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, [open]);

  return ref;
}
