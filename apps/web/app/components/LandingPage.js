import Link from "next/link";
import { ArrowDown, ArrowRight, Check, FileText, Lightning, TelegramLogo, WarningCircle } from "@phosphor-icons/react";
import { PublicFooter, PublicNav, PublicTrustNote } from "./PublicShell";

export default function LandingPage() {
  return <main className="public-shell">
    <PublicNav />
    <div className="public-content">
      <section className="landing-hero" aria-labelledby="landing-title">
        <div className="landing-hero-copy"><span className="public-kicker">SISTEM KEUANGAN KELUARGA</span><h1 id="landing-title">Keuangan keluarga,<br/><em>tanpa menebak.</em></h1><p className="landing-lede">Richmod menyatukan transaksi dari email bank, Telegram, dan dokumen keuangan menjadi satu catatan keluarga. Saat informasinya belum cukup jelas, Richmod bertanya—bukan mengarang.</p><div className="landing-actions"><Link className="button public-button-primary" href="/login">Masuk ke Richmod <ArrowRight aria-hidden="true"/></Link><a className="public-text-link" href="#cara-kerja">Lihat cara kerjanya <ArrowDown aria-hidden="true"/></a></div><PublicTrustNote /></div>
        <img className="landing-evidence-flow" src="/brand/richmod-evidence-flow.svg" alt="Alur Richmod dari bukti ke catatan atau keputusan manusia" />
      </section>

      <section className="landing-intro" id="cara-kerja"><h2>Setiap angka punya asal.</h2><p>Richmod menjaga hubungan antara bukti, pemahaman, dan catatan finansial—supaya kamu tidak hanya melihat total, tapi mengerti dari mana datangnya.</p></section>

      <section className="workflow-section" aria-labelledby="workflow-title"><div className="section-heading"><span className="public-kicker">CARA KERJA</span><h2 id="workflow-title">Dari bukti menjadi keputusan yang bisa ditinjau.</h2></div><div className="workflow-rail"><WorkflowStep icon={<TelegramLogo aria-hidden="true"/>} number="01" title="Bukti masuk" copy="Email bank, pesan Telegram, atau dokumen yang kamu kirim."/><ArrowRight className="workflow-arrow" aria-hidden="true"/><WorkflowStep icon={<Lightning aria-hidden="true"/>} number="02" title="Richmod memahami" copy="Model bahasa membaca konteks. Richmod memeriksa hasilnya dengan aturan yang konsisten."/><ArrowRight className="workflow-arrow" aria-hidden="true"/><WorkflowStep icon={<Check aria-hidden="true"/>} number="03" title="Ledger tercatat" copy="Yang cukup jelas masuk ke riwayat keluarga dengan sumbernya."/></div><div className="workflow-review"><WarningCircle aria-hidden="true"/><div><strong>Belum cukup jelas?</strong><span>Richmod mengirimkannya ke Review Inbox—keputusan tetap di tangan kamu.</span></div><Link href="/login">Lihat produk <ArrowRight aria-hidden="true"/></Link></div></section>

      <section className="trust-section" id="kepercayaan" aria-labelledby="trust-title"><div className="trust-copy"><span className="public-kicker">AI MEMBANTU. SISTEM MEMUTUSKAN.</span><h2 id="trust-title">Otomasi yang tahu kapan harus berhenti.</h2><p>Model bahasa membantu membaca input yang tidak terstruktur. Ia tidak punya akses untuk mengubah ledger. Richmod memeriksa hasilnya; hal yang ambigu menunggu keputusan manusia.</p><Link className="public-text-link" href="/privacy">Baca tentang privasi <ArrowRight aria-hidden="true"/></Link></div><div className="evidence-stack" aria-label="Hubungan bukti, interpretasi, dan ledger"><div className="stack-item"><FileText aria-hidden="true"/><div><span>Bukti asli</span><strong>Bank Jago · email transaksi</strong><small>6 Sep 2026 · 09:20 WIB</small></div></div><div className="stack-connector" aria-hidden="true">↓</div><div className="stack-item"><Lightning aria-hidden="true"/><div><span>Interpretasi tervalidasi</span><strong>Pasar Minggu · Rp185.000</strong><small>Pengeluaran · Belanja rumah</small></div></div><div className="stack-connector" aria-hidden="true">↓</div><div className="stack-item stack-item-final"><Check aria-hidden="true"/><div><span>Ledger keluarga</span><strong>Tercatat dan dapat ditelusuri</strong><small>Sumber dan status tetap terlihat</small></div></div></div></section>

      <section className="household-section" aria-labelledby="household-title"><div><h2 id="household-title">Satu catatan untuk memahami uang keluarga bersama.</h2></div><div className="household-points"><p><strong>Anggota tetap terlihat.</strong> Catatan dapat menunjukkan siapa yang terkait tanpa memisahkan riwayat keuangan rumah.</p><p><strong>Sumber tetap jelas.</strong> Bukti dan status membantu mencegah catatan ganda atau angka yang tidak bisa dijelaskan.</p></div></section>

      <section className="review-section" aria-labelledby="review-title"><div className="review-copy"><span className="public-kicker">KOTAK TINJAUAN</span><h2 id="review-title">Kalau Richmod tidak yakin, keputusan tetap di tangan kamu.</h2><p>Bukan notifikasi yang menumpuk. Hanya hal yang benar-benar membutuhkan konteks dari rumahmu.</p></div><article className="review-example"><div className="review-example-top"><span><WarningCircle aria-hidden="true"/> Butuh keputusan</span><small>TRANSFER · 15:00 WIB</small></div><h3>Transfer Rp750.000</h3><p>Tujuan transfer belum cukup jelas untuk dikategorikan.</p><div className="review-options"><span>Rekening sendiri</span><span>Rekening rumah tangga</span><span>Catat sebagai pengeluaran</span></div><small className="review-source">Sumber: email bank · 5 Sep 2026</small></article></section>

      <section className="landing-cta"><h2>Mulai dari riwayat yang bisa kamu percaya.</h2><Link className="button public-button-primary" href="/login">Masuk ke Richmod <ArrowRight aria-hidden="true"/></Link></section>
    </div><PublicFooter />
  </main>;
}

function WorkflowStep({ icon, number, title, copy }) {
  return <article className="workflow-step"><span className="workflow-number">{number}</span><div className="workflow-icon">{icon}</div><h3>{title}</h3><p>{copy}</p></article>;
}
