const MESSAGES = {
  invite_required: "Akun ini menunggu aktivasi undangan. Buka tautan undangan dari pemilik keluarga atau administrator terlebih dahulu.",
  google_email_mismatch: "Email Google tidak cocok dengan email pada undangan. Gunakan akun Google yang sesuai.",
  invite_invalid: "Undangan sudah kedaluwarsa atau tidak berlaku. Minta pemilik keluarga membuat tautan baru.",
  google_denied: "Masuk dengan Google dibatalkan.",
  google_unavailable: "Masuk dengan Google belum tersedia saat ini. Gunakan email dan kata sandi.",
  google_failed: "Masuk dengan Google belum berhasil. Coba lagi.",
};

export function googleLoginErrorMessage(code) {
  if (!code) return "";
  return Object.hasOwn(MESSAGES, code) ? MESSAGES[code] : "Tidak dapat masuk saat ini. Coba lagi.";
}

export const GOOGLE_INVITE_START_PATH = "/api/v1/auth/google/invite";

export function googleStartHref() {
  return "/api/v1/auth/google/start";
}
