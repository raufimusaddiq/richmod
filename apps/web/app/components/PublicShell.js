import Link from "next/link";
import { ArrowUpRight, LockKey, ShieldCheck } from "@phosphor-icons/react";

export function PublicNav({ hideLogin = false }) {
  return <header className="public-nav-wrap"><nav className="public-nav" aria-label="Navigasi publik">
    <Link className="public-brand" href="/" aria-label="Richmod beranda"><span>R</span><strong>Richmod</strong></Link>
    <div className="public-nav-links"><a href="/#cara-kerja">Cara kerja</a><a href="/#kepercayaan">Kepercayaan</a><Link href="/privacy">Privasi</Link></div>
    {!hideLogin && <Link className="public-nav-login" href="/login">Masuk <ArrowUpRight aria-hidden="true"/></Link>}
  </nav></header>;
}

export function PublicFooter() {
  return <footer className="public-footer"><div><Link className="public-brand" href="/" aria-label="Richmod beranda"><span>R</span><strong>Richmod</strong></Link><p>Keuangan keluarga, tanpa menebak.</p></div><nav aria-label="Tautan publik"><Link href="/privacy">Privasi</Link><Link href="/terms">Ketentuan</Link><Link href="/login">Masuk</Link></nav><small><ShieldCheck aria-hidden="true"/> Bukti tetap terhubung dengan catatan</small></footer>;
}

export function PublicTrustNote() {
  return <p className="public-trust-note"><LockKey aria-hidden="true"/> Model membantu memahami. Richmod yang memutuskan.</p>;
}
