import { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users } from "@phosphor-icons/react";
import { CATEGORY_COLORS, OTHER_CATEGORY_COLOR } from "../lib/categoryColors";
import { useCategoryPalette, useCategoryParents } from "./CategoryPaletteProvider";
import { categoryVisual } from "../lib/categoryIcons";

const icons = { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users };

export default function CategoryIcon({ category, color = "", size = 34 }) {
  const palette = useCategoryPalette();
  const parents = useCategoryParents();
  const { icon, tone, color: resolved } = categoryVisual(category, palette, parents);
  const Icon = icons[icon] || Tag;
  const activeColor = color && color !== OTHER_CATEGORY_COLOR ? color : resolved;
  const matchedIndex = CATEGORY_COLORS.indexOf(activeColor);
  const activeTone = matchedIndex >= 0 ? matchedIndex + 1 : activeColor === OTHER_CATEGORY_COLOR ? "other" : tone;
  const style = activeColor ? { "--icon-size": `${size}px`, "--cat-color": activeColor } : { "--icon-size": `${size}px` };
  return <span className={`category-icon tone-${activeTone}`} style={style} aria-hidden="true"><Icon weight="duotone"/></span>;
}
