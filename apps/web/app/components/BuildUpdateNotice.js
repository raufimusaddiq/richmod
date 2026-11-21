"use client";

import { useEffect, useRef, useState } from "react";
import { BUILD_ID } from "../lib/buildId.generated.js";
import { fetchDeployedId, startBuildWatch } from "../lib/buildWatch";

let dismissedForThisLoad = false;

export default function BuildUpdateNotice() {
  const [available, setAvailable] = useState(false);
  const watch = useRef(null);

  useEffect(() => {
    if (process.env.NODE_ENV !== "production" || !BUILD_ID || dismissedForThisLoad) return;
    watch.current = startBuildWatch({ ownId: BUILD_ID, fetchDeployedId, onUpdate: () => setAvailable(true) });
    return () => watch.current?.stop();
  }, []);

  if (!available) return null;
  return <div className="build-update">
    <div className="build-update-body">
      <p role="status">Versi baru tersedia.</p>
      <button type="button" onClick={() => window.location.reload()}>Muat ulang</button>
    </div>
    <button type="button" className="build-update-dismiss" aria-label="Tutup pemberitahuan versi baru" onClick={() => {
      dismissedForThisLoad = true;
      watch.current?.dismiss();
      setAvailable(false);
    }}><span aria-hidden="true">×</span></button>
  </div>;
}
