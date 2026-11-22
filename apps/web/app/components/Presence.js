import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { initialPresence, nextPresence, whenSettled } from "./presenceState";

function restorableOpener(wrapper, opener) {
  if (!(opener instanceof HTMLElement) || opener === document.body || !opener.isConnected) return null;
  if (wrapper?.contains(opener) || opener.closest("[inert]")) return null;
  return opener;
}

export default function Presence({ show, children }) {
  const wrapper = useRef(null);
  const last = useRef(null);
  const opener = useRef(null);
  const [state, setState] = useState(() => initialPresence(Boolean(show)));
  const next = nextPresence(state, show ? "show" : "hide");
  if (next !== state) setState(next);
  if (show) last.current = children();
  const leaving = next.phase === "leaving";

  useLayoutEffect(() => {
    if (next.phase !== "open") return;
    const active = document.activeElement;
    opener.current = wrapper.current?.contains(active) ? null : active;
  }, [next.phase, next.generation]);

  useLayoutEffect(() => {
    if (!leaving) return;
    const active = document.activeElement;
    if (active && active !== document.body && !wrapper.current?.contains(active)) return;
    restorableOpener(wrapper.current, opener.current)?.focus();
  }, [leaving]);

  useEffect(() => {
    if (!leaving) return undefined;
    wrapper.current?.querySelectorAll("dialog[open]").forEach(dialog => dialog.close());
    return whenSettled(wrapper.current, () => { last.current = null; setState(current => nextPresence(current, "settled")); });
  }, [leaving]);

  if (next.phase === "gone" || last.current === null) return null;
  return <div key={next.generation} ref={wrapper} className="presence" data-presence={leaving ? "leaving" : "open"} inert={leaving}>{last.current}</div>;
}
