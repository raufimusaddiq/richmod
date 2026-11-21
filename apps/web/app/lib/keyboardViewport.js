const NON_KEYBOARD_INPUT_TYPES = new Set([
  "button",
  "checkbox",
  "color",
  "file",
  "hidden",
  "image",
  "radio",
  "range",
  "reset",
  "submit",
]);

export function isKeyboardField(element) {
  if (!element || typeof element !== "object") return false;
  const tag = String(element.tagName || "").toUpperCase();
  if (element.disabled || element.readOnly) return false;
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag !== "INPUT") return false;
  const type = String(element.type || "text").toLowerCase();
  return !NON_KEYBOARD_INPUT_TYPES.has(type);
}

export function measureVisualViewport(win) {
  const innerHeight = Math.max(0, Number(win?.innerHeight) || 0);
  const vv = win?.visualViewport;
  if (!vv) return { inset: 0, height: Math.round(innerHeight) };
  const rawHeight = Number(vv.height);
  const height = Math.max(0, Math.round(Number.isFinite(rawHeight) && rawHeight > 0 ? rawHeight : innerHeight));
  const offsetTop = Number(vv.offsetTop) || 0;
  const scale = Number(vv.scale) || 1;
  const inset = scale > 1.05 ? 0 : Math.max(0, Math.round(innerHeight - height - offsetTop));
  return { inset, height };
}

function centerField(element) {
  if (!isKeyboardField(element)) return;
  if (typeof element.scrollIntoView === "function") element.scrollIntoView({ block: "center" });
}

export function attachKeyboardViewport(win = typeof window !== "undefined" ? window : null, doc = typeof document !== "undefined" ? document : null) {
  const root = doc?.documentElement;
  if (!win || !root) return () => {};
  const vv = win.visualViewport;
  let lastHeight = -1;

  const apply = (scrollActive = false) => {
    const { inset, height } = measureVisualViewport(win);
    root.style.setProperty("--kb-inset", `${inset}px`);
    if (height > 0) root.style.setProperty("--vv-height", `${height}px`);
    root.classList?.toggle("kb-open", inset > 0);
    const heightChanged = lastHeight !== -1 && height !== lastHeight;
    lastHeight = height;
    if (scrollActive && (heightChanged || inset > 0)) centerField(doc.activeElement);
  };

  const onViewportResize = () => apply(true);
  const onViewportScroll = () => apply(false);
  const onFocusIn = event => {
    const active = isKeyboardField(event?.target);
    root.classList?.toggle("kb-focused", active);
    if (active) centerField(event.target);
  };
  const onFocusOut = event => {
    root.classList?.toggle("kb-focused", isKeyboardField(event?.relatedTarget));
  };

  apply(false);
  root.classList?.toggle("kb-focused", isKeyboardField(doc.activeElement));
  vv?.addEventListener?.("resize", onViewportResize);
  vv?.addEventListener?.("scroll", onViewportScroll);
  win.addEventListener?.("resize", onViewportResize);
  doc.addEventListener?.("focusin", onFocusIn);
  doc.addEventListener?.("focusout", onFocusOut);

  return () => {
    vv?.removeEventListener?.("resize", onViewportResize);
    vv?.removeEventListener?.("scroll", onViewportScroll);
    win.removeEventListener?.("resize", onViewportResize);
    doc.removeEventListener?.("focusin", onFocusIn);
    doc.removeEventListener?.("focusout", onFocusOut);
    root.style.removeProperty("--kb-inset");
    root.style.removeProperty("--vv-height");
    root.classList?.remove("kb-open");
    root.classList?.remove("kb-focused");
  };
}
