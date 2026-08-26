"use client";

import { useEffect, useState } from "react";

export default function useAuth(redirect = true) {
  const [user, setUser] = useState(null);
  useEffect(() => {
    const unavailable = () => {
      setUser(false);
      if (redirect) window.location.replace("/");
    };
    fetch("/api/v1/auth/me").then(async response => {
      if (!response.ok) {
        unavailable();
        return;
      }
      setUser(await response.json());
    }).catch(unavailable);
  }, [redirect]);
  return user;
}
