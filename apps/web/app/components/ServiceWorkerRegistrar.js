"use client";

import { useEffect } from "react";

// Registers the build-stamped service worker served by webui.go. Production
// only: `next dev` has no out/sw.js, and a dev worker intercepting chunks would
// poison the browser profile.
export default function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) return;
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
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  return null;
}
