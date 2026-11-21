import { toRupiahInt } from "./home.js";

const dayKey = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Jakarta", year: "numeric", month: "2-digit", day: "2-digit" });
const dayName = new Intl.DateTimeFormat("id-ID", { timeZone: "Asia/Jakarta", weekday: "long", day: "numeric", month: "short" });
const clock = new Intl.DateTimeFormat("id-ID", { timeZone: "Asia/Jakarta", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });

export const jakartaDay = value => dayKey.format(new Date(value));
// "Jumat, 20 Nov": the header of one ledger day.
export const dayHeading = value => dayName.format(new Date(value)).replace(/^(\p{L}+)\s/u, "$1, ").replace(/,,/, ",");
// "14.32": time of day only, the day is already in the header.
export const timeOfDay = value => clock.format(new Date(value)).replace(":", ".");

// Groups a newest-first list into Jakarta calendar days. A day's spend is confirmed expenses minus
// confirmed refunds, so needs-review and voided rows never move the subtotal.
export function groupByDay(items = []) {
  const days = [];
  for (const item of items) {
    const key = jakartaDay(item.transactionAt);
    let day = days[days.length - 1];
    if (!day || day.day !== key) { day = { day: key, heading: dayHeading(item.transactionAt), items: [], spend: 0n, reviewCount: 0 }; days.push(day); }
    day.items.push(item);
    if (item.status === "NEEDS_REVIEW") day.reviewCount += 1;
    if (item.status === "CONFIRMED") {
      if (item.type === "EXPENSE") day.spend += toRupiahInt(item.amount);
      else if (item.type === "REFUND") day.spend -= toRupiahInt(item.amount);
    }
  }
  return days.map(day => ({ ...day, spend: day.spend.toString() }));
}
