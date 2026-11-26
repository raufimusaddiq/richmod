const amount = value => (value == null || value === "" ? null : Number(value));

export function buildHeatmap(categoryHistory, history = [], mode = "FULL", { selectedCategory = "", selectable = [] } = {}) {
  const sameDay = mode === "SAME_DAY";
  const source = sameDay ? categoryHistory?.sameDay : categoryHistory;
  const starts = source?.cycleStarts ?? [];
  const rows = source?.rows ?? [];
  const other = source?.other?.amounts ?? [];
  const totals = source?.totals ?? [];
  if (!starts.length || !rows.length || other.length !== starts.length || totals.length !== starts.length) return null;
  if (rows.some(row => row.amounts?.length !== starts.length)) return null;
  const state = new Map(history.map(cycle => [cycle.start, cycle.state]));
  const days = source.measuredDays ?? [];
  const columns = starts.map((start, index) => {
    const running = state.get(start) === "ACTIVE";
    return { start, running, selected: source.selectedIndex === index, untinted: !sameDay && running, subhead: sameDay ? `${running ? "berjalan · " : ""}hari 1–${days[index] ?? "?"}` : running ? "belum penuh" : "" };
  });
  const tints = values => {
    const peak = Math.max(0, ...values.map((value, index) => (columns[index].untinted ? null : amount(value))).filter(value => value != null && Number.isFinite(value)));
    return values.map((value, index) => {
      const number = amount(value);
      return columns[index].untinted || !(peak > 0) || number == null || !Number.isFinite(number) ? 0 : Math.max(0, number) / peak;
    });
  };
  const pickable = new Set(selectable);
  const categories = rows.map(row => {
    const id = row.id || "uncategorized";
    return { key: id, id, name: row.name, amounts: row.amounts, tints: tints(row.amounts), delta: row.deltaVsPrevious, selected: id === selectedCategory, selectable: pickable.has(id) };
  });
  categories.push({ key: "other", id: "", name: "Kategori lain", amounts: other, tints: tints(other), delta: source.other?.deltaVsPrevious, muted: true, selectable: false });
  return { mode: sameDay ? "SAME_DAY" : "FULL", columns, rows: categories, total: { amounts: totals, delta: source.totalDeltaVsPrevious } };
}

export function nonZeroAmount(value) {
  const number = amount(value);
  return number != null && Number.isFinite(number) && number !== 0;
}

export function merchantBars(ranking) {
  if (!ranking || !Array.isArray(ranking.rows)) return null;
  const amounts = [...ranking.rows, ranking.other].map(item => amount(item?.amount));
  const peak = Math.max(0, ...amounts.filter(value => value != null && Number.isFinite(value)));
  const length = value => {
    const number = amount(value);
    return peak > 0 && number != null && Number.isFinite(number) ? Math.max(0, number) / peak : 0;
  };
  return {
    compareLabel: ranking.mode === "ELAPSED_DAYS" ? "Siklus lalu, hari yang sama" : "Siklus lalu",
    rows: ranking.rows.map((row, index) => ({ ...row, key: `${row.id || row.name}:${index}`, length: length(row.amount), notNamed: row.kind === "NONE" })),
    other: ranking.other ? { ...ranking.other, length: length(ranking.other.amount) } : null,
    total: ranking.total ? { ...ranking.total } : null,
  };
}

export function householdRows(members = []) {
  return members.map((member, index) => {
    const rawShare = member.shareOfExpense ?? member.share ?? null;
    return {
      ...member,
      key: member.id || member.name,
      initial: String(member.name || "?").trim().charAt(0).toUpperCase(),
      share: rawShare == null ? null : Number(rawShare),
      shareOfExpense: rawShare,
      tint: index % 4,
      automatic: member.kind === "AUTOMATIC",
    };
  });
}

export function deltaTone(value) {
  if (value == null || value === "") return "none";
  const number = amount(value);
  if (number == null || !Number.isFinite(number) || number === 0) return "none";
  return number < 0 ? "good" : "bad";
}

export function decisionCount(body) {
  return [...String(body ?? "")].length;
}
