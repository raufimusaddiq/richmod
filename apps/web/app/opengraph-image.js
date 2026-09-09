import { ImageResponse } from "next/og";

export const alt = "Richmod — keuangan keluarga, tanpa menebak";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default function OpenGraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: "64px 72px",
          background: "#f3f1ec",
          color: "#282522",
          fontFamily: '"Avenir Next", "Segoe UI", sans-serif',
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
          <div
            style={{
              width: 58,
              height: 58,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              borderRadius: 15,
              background: "#282522",
              color: "#ffffff",
              fontFamily: "Georgia, serif",
              fontSize: 33,
              fontWeight: 700,
            }}
          >
            R
          </div>
          <div style={{ display: "flex", flexDirection: "column" }}>
            <div style={{ fontSize: 30, fontWeight: 700, letterSpacing: "-0.03em" }}>Richmod</div>
            <div style={{ marginTop: 5, color: "#6a625e", fontSize: 16 }}>Keuangan keluarga, bukti sebagai dasar.</div>
          </div>
        </div>

        <div style={{ display: "flex", flexDirection: "column", maxWidth: 900 }}>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              fontSize: 72,
              fontWeight: 700,
              lineHeight: 1.02,
              letterSpacing: "-0.055em",
            }}
          >
            <div>Keuangan keluarga,</div>
            <div>tanpa menebak.</div>
          </div>
          <div style={{ marginTop: 28, color: "#57504d", fontSize: 28, lineHeight: 1.35 }}>
            Bukti masuk. Richmod memahami. Kamu tetap memegang keputusan.
          </div>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 12, fontSize: 18, fontWeight: 650 }}>
          <div style={{ display: "flex", padding: "11px 16px", border: "1px solid #cacbc4", borderRadius: 999, background: "#fbfaf7" }}>
            Email bank
          </div>
          <div style={{ display: "flex", padding: "11px 16px", border: "1px solid #cacbc4", borderRadius: 999, background: "#fbfaf7" }}>
            Telegram
          </div>
          <div style={{ display: "flex", padding: "11px 16px", border: "1px solid #cacbc4", borderRadius: 999, background: "#fbfaf7" }}>
            Dokumen
          </div>
          <div style={{ display: "flex", margin: "0 4px", color: "#6d435c", fontSize: 30 }}>→</div>
          <div style={{ display: "flex", padding: "11px 16px", borderRadius: 999, background: "#eee4ea", color: "#6d435c" }}>
            Catatan keluarga
          </div>
        </div>
      </div>
    ),
    size,
  );
}
