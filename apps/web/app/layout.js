import "./globals.css";

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
