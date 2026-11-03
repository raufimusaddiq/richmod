"use client";

import { useEffect } from "react";

// Registers the pass-through worker served by webui.go. Production only:
// `next dev` has no /sw.js, and a worker registered against the dev server
// would outlive the session. The worker keeps no cache, so registration is all
// that is needed to become installable.
export default function ServiceWorkerRegistrar() {
  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) return;
    navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" }).catch(() => {});
  }, []);
  return null;
}
