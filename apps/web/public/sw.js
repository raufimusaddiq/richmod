// Pass-through service worker that also receives Web Push. It exists so
// Richmod is installable and a notification can arrive while the app is
// closed; it keeps no cache and intercepts no request. HTML, chunks, the
// manifest and the API all come from the network exactly as they would
// without a worker, so no release can leave an installed client on a build
// whose chunks the server deleted. Offline use is not supported.
//
// Older releases cached /_next/static and kept richmod-generations metadata;
// activate removes those now-unused stores so they do not hold storage forever.
// `push` shows a notification even when its payload is missing or unreadable:
// iOS drops a subscription after repeated pushes that show nothing. A click
// focuses the open window when there is one and only follows a same-origin
// path from the payload.
const FALLBACK = { title: "Richmod", body: "Ada pembaruan di kotak masuk." };

function sameOriginPath(value) {
  try {
    const url = new URL(typeof value === "string" && value ? value : "/inbox", self.location.origin);
    return url.origin === self.location.origin ? `${url.pathname}${url.search}${url.hash}` : "/inbox";
  } catch {
    return "/inbox";
  }
}

function notificationFrom(payload) {
  const body = payload && typeof payload === "object" ? payload : {};
  return {
    title: typeof body.title === "string" && body.title.trim() ? body.title : FALLBACK.title,
    body: typeof body.body === "string" && body.body.trim() ? body.body : FALLBACK.body,
    url: sameOriginPath(body.url),
  };
}

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", event => {
  event.waitUntil((async () => {
    try {
      for (const key of await caches.keys()) {
        if (key.startsWith("richmod-static-") || key === "richmod-generations") await caches.delete(key);
      }
    } catch {
      // Cache Storage may be unavailable; activation must still complete.
    }
    await self.clients.claim();
  })());
});

self.addEventListener("push", event => {
  event.waitUntil((async () => {
    let payload = null;
    try {
      payload = event.data ? await event.data.json() : null;
    } catch {
      // A malformed body is not a reason to stay silent; use the fallback.
    }
    const { title, body, url } = notificationFrom(payload);
    await self.registration.showNotification(title, {
      body,
      icon: "/icons/icon-192.png",
      data: { url },
    });
  })());
});

self.addEventListener("notificationclick", event => {
  event.notification.close();
  const target = sameOriginPath(event.notification?.data?.url);
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const existing = windows.find(client => client.url.startsWith(self.location.origin));
    if (existing) {
      await existing.focus();
      if (typeof existing.navigate === "function") await existing.navigate(target);
      return;
    }
    await self.clients.openWindow(target);
  })());
});
