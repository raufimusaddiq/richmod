import "./globals.css";
import "./detail-drawer.css";
import AuthProvider from "./components/AuthProvider";
import InboxCountProvider from "./components/InboxCountProvider";
import ServiceWorkerRegistrar from "./components/ServiceWorkerRegistrar";


// viewport-fit=cover makes the safe-area insets real (Home Screen app, landscape).
export const viewport = { width: "device-width", initialScale: 1, viewportFit: "cover", themeColor: "#245C54" };

const socialTitle = "Richmod · Keuangan keluarga, tanpa menebak";
const socialDescription = "Bukti masuk. Richmod memahami. Kamu tetap memegang keputusan.";

export const metadata = {
  metadataBase: new URL("https://finance.investdx.biz.id"),
  title: socialTitle,
  description: "Richmod menyatukan bukti keuangan rumah tangga menjadi satu ledger keluarga yang dapat dijelaskan.",
  manifest: "/manifest.webmanifest",
  // Read by ServiceWorkerRegistrar so a window can name its cache generation
  // exactly; script URLs are shared between builds and cannot identify one.
  other: { "richmod-build": process.env.NEXT_PUBLIC_RICHMOD_BUILD || "dev" },
  // iOS ignores the manifest for install and reads these instead.
  appleWebApp: { capable: true, title: "Richmod", statusBarStyle: "default" },
  icons: {
    icon: [{ url: "/icon.svg", type: "image/svg+xml" }],
    apple: [{ url: "/icons/apple-touch-icon-180.png", sizes: "180x180", type: "image/png" }],
  },
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
      <body><meta name="apple-mobile-web-app-capable" content="yes" /><ServiceWorkerRegistrar /><AuthProvider><InboxCountProvider>{children}</InboxCountProvider></AuthProvider></body>
    </html>
  );
}
