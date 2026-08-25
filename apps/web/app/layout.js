import "./globals.css";

export const metadata = {
  title: "Family Finance",
  description: "Household financial inbox",
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
