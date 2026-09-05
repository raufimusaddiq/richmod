import Link from "next/link";

export const metadata = {
  title: "Kebijakan Privasi · Richmod",
  description: "Kebijakan privasi Richmod Family Finance.",
};

export default function PrivacyPage() {
  return <main className="public-legal"><article className="surface public-legal-card">
    <Link className="public-legal-brand" href="/">Richmod</Link>
    <span className="eyebrow">KEBIJAKAN PRIVASI</span>
    <h1>Kebijakan Privasi</h1>
    <p className="public-legal-updated">Terakhir diperbarui: 4 September 2026</p>
    <p>Richmod membantu household mencatat dan memahami pemasukan serta pengeluaran. Halaman ini menjelaskan data yang kami proses dan cara kami menjaganya.</p>
    <h2>Data yang diproses</h2>
    <p>Kami memproses data akun, data household, transaksi, dokumen yang Anda kirimkan, serta integrasi email atau Telegram yang Anda aktifkan. Data tersebut digunakan hanya untuk menyediakan fitur pencatatan, review, dan analisis keuangan.</p>
    <h2>Notifikasi email keuangan</h2>
    <p>Richmod dapat memberi Anda alamat penerusan khusus untuk notifikasi keuangan. Email mentah yang diteruskan disimpan sebagai bukti pada penyimpanan terpisah dan hanya diproses bila alamat household, pengirim listener, dan bukti autentikasi email lolos pemeriksaan.</p>
    <h2>Penyimpanan dan keamanan</h2>
    <p>Data keuangan disimpan dengan pembatasan berbasis household. Dokumen dan backup disimpan terenkripsi pada penyimpanan terpisah. Kami tidak menjual data pribadi atau menggunakannya untuk iklan.</p>
    <h2>Kontak</h2>
    <p>Untuk pertanyaan privasi atau permintaan penghapusan akun, hubungi administrator household melalui kanal dukungan Richmod.</p>
    <nav className="public-legal-links"><Link href="/terms">Ketentuan Layanan</Link><Link href="/">Kembali ke Richmod</Link></nav>
  </article></main>;
}
