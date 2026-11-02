/**
 * The app is exported as static files and embedded in the Go API binary, so a
 * single container serves both. Static export cannot emit per-route headers;
 * apps/api/internal/webui sends the equivalent Cache-Control per route class.
 */
const nextConfig = {
  output: "export",
};

export default nextConfig;
