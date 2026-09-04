"use client";

import { useEffect } from "react";
import { useAuthContext } from "./AuthProvider";

export default function useAuth(redirect = true) {
  const { user, loaded } = useAuthContext();

  useEffect(() => {
    if (loaded && user === false && redirect) {
      window.location.replace("/");
    }
  }, [loaded, redirect, user]);

  return loaded ? user : null;
}
