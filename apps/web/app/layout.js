import "./globals.css";
import AuthProvider from "./components/AuthProvider";

// The browser-authenticated app must not reuse an HTML shell from an older
// deployment whose client chunks may have been replaced.
export const dynamic = "force-dynamic";

export const metadata = {
  title: "Richmod · Keuangan keluarga, tanpa menebak",
  description: "Richmod menyatukan bukti keuangan rumah tangga menjadi satu ledger keluarga yang dapat dijelaskan.",
  openGraph: { title: "Richmod · Keuangan keluarga, tanpa menebak", description: "Bukti masuk. Richmod memahami. Kamu tetap memegang keputusan." },
};

export default function RootLayout({ children }) {
  return (
    <html lang="id">
      <body><AuthProvider>{children}</AuthProvider></body>
    </html>
  );
}
