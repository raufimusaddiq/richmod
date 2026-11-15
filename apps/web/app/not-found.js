import Link from "next/link";

export const metadata = {
  title: "Halaman tidak ditemukan · Richmod",
  description: "Halaman yang kamu cari tidak ada atau sudah dipindahkan.",
};

export default function NotFound() {
  return <>
    <header className="public-nav-wrap"><nav className="public-nav" aria-label="Navigasi publik"><Link className="public-brand" href="/" aria-label="Richmod beranda"><span>R</span><strong>Richmod</strong></Link></nav></header>
    <main id="main" className="public-legal not-found"><section className="surface public-legal-card not-found-card">
      <span className="eyebrow">404</span>
      <h1>Halaman tidak ditemukan</h1>
      <p>Alamat yang kamu buka tidak ada atau sudah dipindahkan. Data keuanganmu tidak terpengaruh.</p>
      <nav className="public-legal-links"><Link className="button" href="/">Kembali ke Beranda</Link></nav>
    </section></main>
  </>;
}
