export const siteOrigin = "https://finance.musaddiqr.com";
export const themeColor = "#245C54";

const socialTitle = "Richmod · Keuangan keluarga, tanpa menebak";
const defaultDescription = "Richmod menyatukan bukti keuangan rumah tangga menjadi satu ledger keluarga yang dapat dijelaskan.";

export const siteMeta = {
  title: socialTitle,
  description: defaultDescription,
  social: {
    title: socialTitle,
    description: "Bukti masuk. Richmod memahami. Kamu tetap memegang keputusan.",
    siteName: "Richmod",
    locale: "id_ID",
    image: { path: "/opengraph-image", width: 1200, height: 630, alt: "Richmod — keuangan keluarga, tanpa menebak" },
  },
};

export const routeMeta = {
  "/": {},
  "/transactions": { title: "Transaksi · Richmod", description: "Daftar transaksi rumah tangga: cari, saring, dan tinjau bukti setiap transaksi." },
  "/analytics": { title: "Laporan · Richmod", description: "Laporan arus kas siklus gaji dan kalender serta kekayaan bersih keluarga." },
  "/documents": { title: "Dokumen · Richmod", description: "Dokumen keuangan yang diunggah dan hasil ekstraksinya." },
  "/household": { title: "Keluarga · Richmod", description: "Anggota keluarga, undangan, dan koneksi Telegram." },
  "/settings": { title: "Pengaturan · Richmod", description: "Kategori, sumber email, integrasi, dan preferensi rumah tangga." },
  "/wealth": { title: "Kekayaan · Richmod", description: "Posisi kekayaan bersih keluarga, riwayat catatan, dan rekap siklus tabungan." },
  "/inbox": { title: "Tinjauan · Richmod", description: "Transaksi yang perlu dikonfirmasi sebelum masuk ke ledger." },
  "/admin": { title: "Admin · Richmod", description: "Konsol operasional platform." },
  "/login": { title: "Masuk · Richmod", description: "Masuk ke Richmod untuk melihat keuangan keluarga." },
  "/invite": { title: "Undangan · Richmod", description: "Terima undangan untuk bergabung ke keluarga di Richmod." },
  "/privacy": { title: "Kebijakan Privasi · Richmod", description: "Kebijakan privasi Richmod Family Finance." },
  "/terms": { title: "Ketentuan Layanan · Richmod", description: "Ketentuan layanan Richmod Family Finance." },
  "/actions": { redirect: "/inbox?view=actions" },
  "/reviews": { redirect: "/inbox?view=transactions" },
};

export const notFoundMeta = {
  title: "Halaman tidak ditemukan · Richmod",
  description: "Halaman yang kamu cari tidak ada atau sudah dipindahkan.",
};

export function metaFor(pathname) {
  const entry = routeMeta[pathname];
  if (!entry) return null;
  return { title: siteMeta.title, description: siteMeta.description, ...entry };
}
