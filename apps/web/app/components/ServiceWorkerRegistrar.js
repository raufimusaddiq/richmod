"use client";

import { useEffect } from "react";

// Registers the build-stamped service worker served by webui.go. Production
// only: `next dev` has no out/sw.js, and a dev worker intercepting chunks would
// poison the browser profile.
export default function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) return;
    // The worker asks which build this window is running so it does not prune a
    // cache an open tab still needs. The first static script is this build's
    // own chunk; the worker maps it back to its generation.
    const claim = event => {
      if (event.data?.type !== "richmod:generation" || !event.ports?.[0]) return;
      const chunks = [...document.scripts]
        .map(script => script.src)
        .filter(src => src.includes("/_next/static/"));
      event.ports[0].postMessage(chunks);
    };
    navigator.serviceWorker.addEventListener("message", claim);
    let registration;
    // An installed PWA can stay open for days without navigating, so re-check
    // for a new build whenever it comes back to the foreground.
    const update = () => {
      if (document.visibilityState === "visible") registration?.update().catch(() => {});
    };
    navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" }).then(value => {
      registration = value;
      document.addEventListener("visibilitychange", update);
    }).catch(() => {});
    return () => {
      navigator.serviceWorker.removeEventListener("message", claim);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);
  return null;
}
