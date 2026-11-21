import { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users } from "@phosphor-icons/react";
import { categoryVisual } from "../lib/categoryIcons";

const icons = { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users };

// A category's Phosphor icon in its tone. Decorative: the category name is always printed beside it.
export default function CategoryIcon({ category, size = 34 }) {
  const { icon, tone } = categoryVisual(category);
  const Icon = icons[icon] || Tag;
  return <span className={`category-icon tone-${tone}`} style={{ "--icon-size": `${size}px` }} aria-hidden="true"><Icon weight="duotone"/></span>;
}
