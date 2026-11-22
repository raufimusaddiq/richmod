"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import { useAuthContext } from "./AuthProvider";
import { buildCategoryPalette, buildCategoryParents } from "../lib/categoryColors";

const EMPTY_MAP = new Map();
const CategoryPaletteContext = createContext(EMPTY_MAP);
const CategoryParentsContext = createContext(EMPTY_MAP);

export default function CategoryPaletteProvider({ children }) {
  const { user } = useAuthContext();
  const [ranked, setRanked] = useState([]);
  const [categories, setCategories] = useState([]);
  const signedIn = Boolean(user?.household);

  const load = useCallback(signal => {
    fetch("/api/v1/analytics/categories?period=last_3_cycles", { signal })
      .then(response => (response.ok ? response.json() : null))
      .then(list => { if (!signal?.aborted && Array.isArray(list)) setRanked(list); })
      .catch(() => {});
    fetch("/api/v1/categories", { signal })
      .then(response => (response.ok ? response.json() : null))
      .then(list => { if (!signal?.aborted && Array.isArray(list)) setCategories(list); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!signedIn) {
      setRanked([]);
      setCategories([]);
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
  const parents = useMemo(() => buildCategoryParents(categories), [categories]);
  return <CategoryPaletteContext.Provider value={palette}><CategoryParentsContext.Provider value={parents}>{children}</CategoryParentsContext.Provider></CategoryPaletteContext.Provider>;
}

export function useCategoryPalette() {
  return useContext(CategoryPaletteContext);
}

export function useCategoryParents() {
  return useContext(CategoryParentsContext);
}
