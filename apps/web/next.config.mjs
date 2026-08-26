/**
 * App routes authenticate in the browser and must always receive the current
 * deployment shell. Long-lived CDN/browser caching can retain an HTML shell
 * whose Next build chunks were removed by the next release.
 */
const appRoutes = ["/", "/transactions", "/analytics", "/reviews", "/documents", "/household", "/settings"];

const nextConfig = {
  async headers() {
    return appRoutes.map(source => ({
      source,
      headers: [{ key: "Cache-Control", value: "no-store, max-age=0" }],
    }));
  },
};

export default nextConfig;
