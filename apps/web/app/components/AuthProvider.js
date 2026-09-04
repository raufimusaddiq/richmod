"use client";

import { createContext, useContext, useEffect, useState } from "react";

const AuthContext = createContext({ user: null, loaded: false });

export default function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let active = true;

    async function load() {
      try {
        const response = await fetch("/api/v1/auth/me");
        const nextUser = response.ok ? await response.json() : false;
        if (active) {
          setUser(nextUser);
          setLoaded(true);
        }
      } catch {
        if (active) {
          setUser(false);
          setLoaded(true);
        }
      }
    }

    load();
    return () => {
      active = false;
    };
  }, []);

  return (
    <AuthContext.Provider value={{ user, loaded }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuthContext() {
  return useContext(AuthContext);
}
