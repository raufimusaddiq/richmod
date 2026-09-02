import Link from "next/link";

export const metadata = {
  title: "Ketentuan Layanan · Richmod",
  description: "Ketentuan layanan Richmod Family Finance.",
};

export default function TermsPage() {
  return <main className="public-legal"><article className="surface public-legal-card">
    <Link className="public-legal-brand" href="/">Richmod</Link>
    <span className="eyebrow">KETENTUAN LAYANAN</span>
    <h1>Ketentuan Layanan</h1>
    <p className="public-legal-updated">Terakhir diperbarui: 2 September 2026</p>
    <p>Dengan menggunakan Richmod, Anda menyetujui ketentuan ini. Richmod adalah alat bantu pencatatan keuangan household, bukan penasihat keuangan, layanan pembayaran, atau pengganti pemeriksaan rekening Anda.</p>
    <h2>Akun dan akses</h2>
    <p>Anda bertanggung jawab menjaga kredensial akun dan hanya menghubungkan layanan yang Anda berwenang gunakan. Akses household harus diberikan kepada anggota yang tepat.</p>
    <h2>Data dan keputusan</h2>
    <p>Richmod dapat menggunakan otomasi dan model bahasa untuk membantu membaca data yang Anda kirimkan. Hasilnya tidak selalu sempurna; periksa Review Inbox dan ledger sebelum mengandalkannya. Keputusan finansial tetap menjadi tanggung jawab Anda.</p>
    <h2>Penggunaan yang wajar</h2>
    <p>Jangan menyalahgunakan layanan, mencoba mengakses household lain, mengirim konten ilegal, atau mengganggu operasional sistem. Kami dapat membatasi akses untuk melindungi pengguna dan data.</p>
    <h2>Perubahan layanan</h2>
    <p>Kami dapat memperbarui fitur dan ketentuan ini. Perubahan material akan ditampilkan melalui layanan atau kanal komunikasi yang tersedia.</p>
    <nav className="public-legal-links"><Link href="/privacy">Kebijakan Privasi</Link><Link href="/">Kembali ke Richmod</Link></nav>
  </article></main>;
}
