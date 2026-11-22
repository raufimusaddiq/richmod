"use client";

import { useEffect, useRef, useState } from "react";
import { initialPresence, nextPresence, whenSettled } from "./presenceState";

export default function Presence({ show, children }) {
  const wrapper = useRef(null);
  const last = useRef(null);
  const [state, setState] = useState(() => initialPresence(Boolean(show)));
  const next = nextPresence(state, show ? "show" : "hide");
  if (next !== state) setState(next);
  if (show) last.current = children();
  const leaving = next.phase === "leaving";

  useEffect(() => {
    if (!leaving) return undefined;
    wrapper.current?.querySelectorAll("dialog[open]").forEach(dialog => dialog.close());
    return whenSettled(wrapper.current, () => { last.current = null; setState(current => nextPresence(current, "settled")); });
  }, [leaving]);

  if (next.phase === "gone" || last.current === null) return null;
  return <div key={next.generation} ref={wrapper} className="presence" data-presence={leaving ? "leaving" : "open"} inert={leaving}>{last.current}</div>;
}
