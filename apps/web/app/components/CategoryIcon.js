"use client";

import { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users } from "@phosphor-icons/react";
import { useCategoryPalette } from "./CategoryPaletteProvider";
import { categoryVisual } from "../lib/categoryIcons";

const icons = { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users };

export default function CategoryIcon({ category, color = "", size = 34 }) {
  const palette = useCategoryPalette();
  const { icon, tone, color: resolved } = categoryVisual(category, palette);
  const Icon = icons[icon] || Tag;
  const activeColor = color || resolved;
  const style = activeColor ? { "--icon-size": `${size}px`, "--cat-color": activeColor } : { "--icon-size": `${size}px` };
  return <span className={`category-icon tone-${tone}`} style={style} aria-hidden="true"><Icon weight="duotone"/></span>;
}
