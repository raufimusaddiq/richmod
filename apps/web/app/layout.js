import "./globals.css";
import AuthProvider from "./components/AuthProvider";

// The browser-authenticated app must not reuse an HTML shell from an older
// deployment whose client chunks may have been replaced.
export const dynamic = "force-dynamic";

const socialTitle = "Richmod · Keuangan keluarga, tanpa menebak";
const socialDescription = "Bukti masuk. Richmod memahami. Kamu tetap memegang keputusan.";

export const metadata = {
  metadataBase: new URL("https://finance.investdx.biz.id"),
  title: socialTitle,
  description: "Richmod menyatukan bukti keuangan rumah tangga menjadi satu ledger keluarga yang dapat dijelaskan.",
  openGraph: {
    title: socialTitle,
    description: socialDescription,
    siteName: "Richmod",
    locale: "id_ID",
    type: "website",
    images: [
      {
        url: "/opengraph-image",
        width: 1200,
        height: 630,
        alt: "Richmod — keuangan keluarga, tanpa menebak",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: socialTitle,
    description: socialDescription,
    images: ["/opengraph-image"],
  },
};

export default function RootLayout({ children }) {
  return (
    <html lang="id">
      <body><AuthProvider>{children}</AuthProvider></body>
    </html>
  );
}
