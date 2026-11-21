export default function LoadingShell({ variant = "page" }) {
  if (variant === "login") {
    return <main className="login-shell">
      <div className="loading-nav" />
      <div className="login-layout" role="status" aria-live="polite" aria-busy="true">
        <div className="login-context loading-context" aria-hidden="true">
          <span className="loading-line loading-back" />
          <span className="loading-line loading-kicker" />
          <span className="loading-line loading-title" />
          <span className="loading-line loading-title short" />
          <span className="loading-line loading-copy" />
          <span className="loading-line loading-copy short" />
        </div>
        <section className="login-card loading-card" aria-hidden="true">
          <span className="loading-mark" />
          <span className="loading-line loading-kicker" />
          <span className="loading-line loading-heading" />
          <span className="loading-line loading-heading short" />
          <div className="loading-spinner"><div className="spinner" /></div>
        </section>
        <span className="visually-hidden">Memuat Richmod…</span>
      </div>
    </main>;
  }
  return <main className="loading" role="status" aria-live="polite" aria-busy="true">
    <section className="surface loading-card" aria-hidden="true">
      <span className="loading-mark" />
      <span className="loading-line loading-heading" />
      <span className="loading-line loading-heading short" />
      <div className="loading-spinner"><div className="spinner" /></div>
    </section>
    <span className="visually-hidden">Memuat Richmod…</span>
  </main>;
}
