const FALLBACK = { title: "Richmod", body: "Ada pembaruan di kotak masuk." };

function sameOriginPath(value) {
  try {
    const url = new URL(typeof value === "string" && value ? value : "/inbox", self.location.origin);
    if (url.origin !== self.location.origin || url.pathname.startsWith("//")) return "/inbox";
    return `${url.pathname}${url.search}${url.hash}`;
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
