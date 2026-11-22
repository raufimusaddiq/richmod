"use client";

import { useEffect, useRef, useState } from "react";

const EXIT_LIMIT_MS = 700;

export function whenSettled(element, done) {
  let finished = false;
  const finish = () => {
    if (finished) return;
    finished = true;
    window.clearTimeout(timer);
    done();
  };
  const timer = window.setTimeout(finish, EXIT_LIMIT_MS);
  const running = typeof element?.getAnimations === "function" ? element.getAnimations({ subtree: true }) : [];
  if (running.length === 0) finish();
  else Promise.allSettled(running.map(animation => animation.finished)).then(finish);
  return () => { finished = true; window.clearTimeout(timer); };
}

export default function Presence({ show, children }) {
  const wrapper = useRef(null);
  const last = useRef(null);
  const [gone, setGone] = useState(!show);
  if (show && gone) setGone(false);
  if (show) last.current = children();
  const leaving = !show && !gone && last.current !== null;

  useEffect(() => {
    if (!leaving) return undefined;
    wrapper.current?.querySelectorAll("dialog[open]").forEach(dialog => dialog.close());
    return whenSettled(wrapper.current, () => { last.current = null; setGone(true); });
  }, [leaving]);

  if (!show && !leaving) return null;
  return <div ref={wrapper} className="presence" data-presence={show ? "open" : "leaving"} inert={!show}>{last.current}</div>;
}
