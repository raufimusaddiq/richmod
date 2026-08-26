import "./globals.css";

// The browser-authenticated app must not reuse an HTML shell from an older
// deployment whose client chunks may have been replaced.
export const dynamic = "force-dynamic";

export const metadata = {
  title: "Richmod Family Finance",
  description: "Keuangan household yang terverifikasi",
};

export default function RootLayout({ children }) {
  return (
    <html lang="id">
      <body>{children}</body>
    </html>
  );
}
