"use client";

import { createContext, useContext, useEffect, useState } from "react";

const AuthContext = createContext({ user: null, loaded: false });

// A network blip or a 5xx says nothing about the session. Only a 401 means
// "signed out"; anything else keeps whatever we knew and retries.
const retryDelay = attempt => Math.min(30000, 2000 * 2 ** attempt);

export default function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [loaded, setLoaded] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true, timer = 0;

    async function load(attempt) {
      try {
        const response = await fetch("/api/v1/auth/me");
        if (!active) return;
        if (response.status === 401) { setUser(false); setUnavailable(false); setLoaded(true); return; }
        if (!response.ok) throw new Error(`auth ${response.status}`);
        const next = await response.json();
        if (!active) return;
        setUser(next); setUnavailable(false); setLoaded(true);
      } catch {
        if (!active) return;
        setUnavailable(true);
        timer = window.setTimeout(() => load(attempt + 1), retryDelay(attempt));
      }
    }

    load(0);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [attempt]);

  // Never leave a page on a silent "Memuat…": say the session cannot be checked.
  if (unavailable && !loaded) {
    return (
      <main className="loading" role="alert">
        <p>Sesi belum dapat diperiksa. Periksa koneksi, lalu coba lagi.</p>
        <button className="secondary" onClick={() => { setUnavailable(false); setAttempt(count => count + 1); }}>Coba lagi</button>
      </main>
    );
  }

  return (
    <AuthContext.Provider value={{ user, loaded, unavailable }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuthContext() {
  return useContext(AuthContext);
}
