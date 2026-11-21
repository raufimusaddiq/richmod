import { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users } from "@phosphor-icons/react";
import { categoryVisual } from "../lib/categoryIcons";

const icons = { Airplane, Baby, Basket, Car, Coffee, ForkKnife, FirstAidKit, Gift, GraduationCap, Heart, House, Lightning, PawPrint, Receipt, Repeat, ShoppingBag, Tag, Ticket, Users };

export default function CategoryIcon({ category, color = "", size = 34 }) {
  const { icon, tone } = categoryVisual(category);
  const Icon = icons[icon] || Tag;
  const style = color ? { "--icon-size": `${size}px`, "--cat-color": color } : { "--icon-size": `${size}px` };
  return <span className={`category-icon tone-${tone}`} style={style} aria-hidden="true"><Icon weight="duotone"/></span>;
}
