const SLIDE = "[data-slide]";
const STAGGER = "[data-stagger]";
const ACTIVE = ':scope > :is([aria-selected="true"], [aria-pressed="true"], .active)';
const WATCHED = ["class", "aria-selected", "aria-pressed"];

export function slideTarget(container) {
  return container.querySelector(ACTIVE);
}

function release(container) {
  requestAnimationFrame(() => requestAnimationFrame(() => container.removeAttribute("data-slide-snap")));
}

export function sliding(container) {
  return document.getAnimations().some(animation => animation.effect?.target === container && animation.effect.pseudoElement === "::before");
}

export function placeIndicator(container, snap = false) {
  if (container.getClientRects().length === 0) return false;
  const active = slideTarget(container);
  const first = !container.hasAttribute("data-slide-ready");
  if (!active) {
    container.setAttribute("data-slide-none", "");
    container.setAttribute("data-slide-ready", "");
    return true;
  }
  const snapping = snap || first || container.hasAttribute("data-slide-none");
  if (snapping) container.setAttribute("data-slide-snap", "");
  container.removeAttribute("data-slide-none");
  const box = active.getBoundingClientRect();
  const frame = container.getBoundingClientRect();
  const style = container.style;
  style.setProperty("--slide-x", `${box.left - frame.left - container.clientLeft + container.scrollLeft}px`);
  style.setProperty("--slide-y", `${box.top - frame.top - container.clientTop + container.scrollTop}px`);
  style.setProperty("--slide-w", `${box.width}px`);
  style.setProperty("--slide-h", `${box.height}px`);
  container.setAttribute("data-slide-ready", "");
  if (snapping) release(container);
  return true;
}

function enterPanels(container) {
  const selector = container.getAttribute("data-slide-panel");
  if (!selector) return;
  document.querySelectorAll(selector).forEach(panel => {
    panel.removeAttribute("data-panel-enter");
    void panel.offsetWidth;
    panel.setAttribute("data-panel-enter", "");
    const done = event => {
      if (event.target !== panel) return;
      panel.removeAttribute("data-panel-enter");
      panel.removeEventListener("animationend", done);
      panel.removeEventListener("animationcancel", done);
    };
    panel.addEventListener("animationend", done);
    panel.addEventListener("animationcancel", done);
  });
}

export function revealOnce(container, started) {
  if (started.has(container)) return false;
  const items = Array.from(container.querySelectorAll(container.getAttribute("data-stagger") || ":scope > *"));
  if (!items.length) return false;
  started.add(container);
  items.forEach((item, index) => {
    item.style.setProperty("--stagger-i", String(index));
    item.setAttribute("data-stagger-item", "");
  });
  const running = (container.getAnimations?.({ subtree: true }) ?? []).filter(animation => animation.effect?.target?.hasAttribute?.("data-stagger-item"));
  const clear = () => items.forEach(item => item.removeAttribute("data-stagger-item"));
  if (!running.length) clear();
  else Promise.allSettled(running.map(animation => animation.finished)).then(clear);
  return true;
}

export function attachMotion(root = document.body) {
  const started = new WeakSet();
  const selected = new WeakMap();
  const resizer = new ResizeObserver(entries => {
    const touched = new Set();
    for (const { target } of entries) {
      const container = target.matches(SLIDE) ? target : target.closest(SLIDE);
      if (container) touched.add(container);
    }
    touched.forEach(container => placeIndicator(container, !sliding(container)));
  });

  function track(container, snap) {
    resizer.observe(container);
    Array.from(container.children).forEach(child => resizer.observe(child));
    const active = slideTarget(container);
    const previous = selected.get(container);
    const changed = previous !== undefined && previous !== active;
    selected.set(container, active);
    const placed = placeIndicator(container, snap);
    if (placed && changed && active) enterPanels(container);
  }

  function collect(node, slides, lists) {
    if (node.nodeType !== 1) return;
    if (node.matches(SLIDE)) slides.add(node);
    if (node.matches(STAGGER)) lists.add(node);
    node.querySelectorAll(`${SLIDE}, ${STAGGER}`).forEach(match => (match.matches(SLIDE) ? slides : lists).add(match));
  }

  function scan(records) {
    const slides = new Set();
    const lists = new Set();
    for (const record of records) {
      const target = record.target;
      if (target.nodeType !== 1 || !target.isConnected) continue;
      const slide = target.closest(SLIDE);
      if (slide) slides.add(slide);
      if (record.type === "childList") {
        const list = target.closest(STAGGER);
        if (list) lists.add(list);
        record.addedNodes.forEach(node => collect(node, slides, lists));
      }
    }
    slides.forEach(container => track(container, false));
    lists.forEach(container => revealOnce(container, started));
  }

  const observer = new MutationObserver(scan);
  observer.observe(root, { subtree: true, childList: true, attributes: true, attributeFilter: WATCHED });
  const initialSlides = new Set();
  const initialLists = new Set();
  collect(root, initialSlides, initialLists);
  initialSlides.forEach(container => track(container, true));
  initialLists.forEach(container => revealOnce(container, started));
  return () => {
    observer.disconnect();
    resizer.disconnect();
  };
}
