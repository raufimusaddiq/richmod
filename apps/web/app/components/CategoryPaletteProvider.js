"use client";

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { useAuthContext } from "./AuthProvider";
import { buildCategoryPalette } from "../lib/categoryColors";

const EMPTY_PALETTE = new Map();
const CategoryPaletteContext = createContext(EMPTY_PALETTE);

export default function CategoryPaletteProvider({ children }) {
  const { user } = useAuthContext();
  const [categories, setCategories] = useState([]);
  const signedIn = Boolean(user);

  useEffect(() => {
    if (!signedIn) {
      setCategories([]);
      return undefined;
    }
    let active = true;
    fetch("/api/v1/categories")
      .then(response => (response.ok ? response.json() : null))
      .then(list => { if (active && Array.isArray(list)) setCategories(list); })
      .catch(() => {});
    return () => { active = false; };
  }, [signedIn]);

  const palette = useMemo(() => buildCategoryPalette(categories), [categories]);
  return <CategoryPaletteContext.Provider value={palette}>{children}</CategoryPaletteContext.Provider>;
}

export function useCategoryPalette() {
  return useContext(CategoryPaletteContext);
}
