const NAVIGATE_EVENT = "richmod:navigate";

let installed = false;

export function installHistoryNotifier() {
  if (installed || typeof window === "undefined") return;
  installed = true;
  for (const method of ["pushState", "replaceState"]) {
    const original = window.history[method];
    window.history[method] = function patched(...args) {
      const result = original.apply(this, args);
      window.dispatchEvent(new Event(NAVIGATE_EVENT));
      return result;
    };
  }
}

export function subscribeToLocation(listener) {
  installHistoryNotifier();
  window.addEventListener("popstate", listener);
  window.addEventListener(NAVIGATE_EVENT, listener);
  return () => {
    window.removeEventListener("popstate", listener);
    window.removeEventListener(NAVIGATE_EVENT, listener);
  };
}

export function readLocation(fallback) {
  if (typeof window === "undefined") return fallback;
  return { pathname: window.location.pathname, search: window.location.search };
}

export function normalizePath(pathname) {
  if (pathname.length > 1 && pathname.endsWith("/")) return pathname.replace(/\/+$/, "") || "/";
  return pathname;
}

function sameDocument(url) {
  return url.origin === window.location.origin;
}

export function navigate(href, { replace = false } = {}) {
  if (typeof window === "undefined") return;
  installHistoryNotifier();
  const url = new URL(href, window.location.href);
  if (!sameDocument(url)) {
    window.location.assign(url.href);
    return;
  }
  const target = `${url.pathname}${url.search}${url.hash}`;
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  const pathChanged = normalizePath(url.pathname) !== normalizePath(window.location.pathname);
  window.history[replace || target === current ? "replaceState" : "pushState"](null, "", target);
  if (pathChanged && !url.hash) window.scrollTo(0, 0);
}

export function isPlainLeftClick(event) {
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
}
