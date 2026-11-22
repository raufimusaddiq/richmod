"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useAuthContext } from "./AuthProvider";
import { buildCategoryPalette } from "../lib/categoryColors";

const EMPTY_PALETTE = new Map();
const CategoryPaletteContext = createContext(EMPTY_PALETTE);

export default function CategoryPaletteProvider({ children }) {
  const { user } = useAuthContext();
  const [ranked, setRanked] = useState([]);
  const signedIn = Boolean(user);

  const load = useCallback(signal => {
    fetch("/api/v1/analytics/categories?period=last_3_cycles", { signal })
      .then(response => (response.ok ? response.json() : null))
      .then(list => { if (!signal?.aborted && Array.isArray(list)) setRanked(list); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!signedIn) {
      setRanked([]);
      return undefined;
    }
    const controller = new AbortController();
    load(controller.signal);
    const refresh = () => load(controller.signal);
    window.addEventListener("richmod:transaction-created", refresh);
    return () => {
      controller.abort();
      window.removeEventListener("richmod:transaction-created", refresh);
    };
  }, [signedIn, load]);

  const palette = useMemo(() => buildCategoryPalette(ranked), [ranked]);
  return <CategoryPaletteContext.Provider value={palette}>{children}</CategoryPaletteContext.Provider>;
}

export function useCategoryPalette() {
  return useContext(CategoryPaletteContext);
}
