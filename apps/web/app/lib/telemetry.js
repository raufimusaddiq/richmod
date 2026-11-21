// Privacy-safe, fire-and-forget UI telemetry. Only fixed screen/action keys
// (mirrored by the server allow-list) are ever sent: never a path, query, id,
// amount or text. Failures are swallowed and never block the UI.
const SCREENS = ["home", "transactions", "analytics", "wealth", "inbox", "documents", "household", "settings", "admin"];

export function screenForPath(pathname) {
  if (typeof pathname !== "string") return null;
  if (pathname === "/") return "home";
  const segment = pathname.split("/")[1];
  return SCREENS.includes(segment) && segment !== "home" ? segment : null;
}

export function trackEvent(screen, action = "SCREEN_VIEW") {
  if (!SCREENS.includes(screen) || (action !== "SCREEN_VIEW" && action !== "NAV_CLICK")) return;
  try {
    const request = fetch("/api/v1/telemetry/events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ screen, action }),
      keepalive: true,
    });
    if (request && typeof request.catch === "function") request.catch(() => {});
  } catch {
    // Telemetry must never affect the app.
  }
}

export function trackScreenView(pathname) {
  const screen = screenForPath(pathname);
  if (screen) trackEvent(screen, "SCREEN_VIEW");
}
