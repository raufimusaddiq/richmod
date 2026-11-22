export const EXIT_LIMIT_MS = 700;

export function initialPresence(show) {
  return { phase: show ? "open" : "gone", generation: 0 };
}

export function nextPresence(state, event) {
  if (event === "show") {
    if (state.phase === "leaving") return { phase: "open", generation: state.generation + 1 };
    if (state.phase === "gone") return { phase: "open", generation: state.generation };
    return state;
  }
  if (event === "hide") return state.phase === "open" ? { ...state, phase: "leaving" } : state;
  if (event === "settled") return state.phase === "leaving" ? { ...state, phase: "gone" } : state;
  return state;
}

export function whenSettled(element, done, limit = EXIT_LIMIT_MS) {
  let finished = false;
  let timer = null;
  const finish = () => {
    if (finished) return;
    finished = true;
    globalThis.clearTimeout(timer);
    done();
  };
  timer = globalThis.setTimeout(finish, limit);
  const running = typeof element?.getAnimations === "function" ? element.getAnimations({ subtree: true }) : [];
  if (running.length === 0) finish();
  else Promise.allSettled(running.map(animation => animation.finished)).then(finish);
  return () => { finished = true; globalThis.clearTimeout(timer); };
}
