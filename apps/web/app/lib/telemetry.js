const SCREENS = ["home", "transactions", "analytics", "wealth", "inbox", "documents", "household", "settings", "admin"];

export function screenForPath(pathname) {
  if (typeof pathname !== "string") return null;
  if (pathname === "/") return "home";
  const segment = pathname.split("/")[1];
  return SCREENS.includes(segment) && segment !== "home" ? segment : null;
}

export function trackEvent(screen, action = "SCREEN_VIEW") {
  if (!SCREENS.includes(screen) || action !== "SCREEN_VIEW") return;
  try {
    const request = fetch("/api/v1/telemetry/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ screen, action }),
      keepalive: true,
    });
    if (request && typeof request.catch === "function") request.catch(() => {});
  } catch {
  }
}

export function trackScreenView(pathname) {
  const screen = screenForPath(pathname);
  if (screen) trackEvent(screen, "SCREEN_VIEW");
}
