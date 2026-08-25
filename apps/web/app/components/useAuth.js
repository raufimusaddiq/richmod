"use client";

import { useEffect, useState } from "react";

export default function useAuth(redirect = true) {
  const [user, setUser] = useState(null);
  useEffect(() => {
    fetch("/api/v1/auth/me").then(async response => {
      if (!response.ok) {
        if (redirect) window.location.href = "/";
        setUser(false);
        return;
      }
      setUser(await response.json());
    }).catch(() => setUser(false));
  }, [redirect]);
  return user;
}
