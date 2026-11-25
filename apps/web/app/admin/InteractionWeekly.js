import { useEffect, useState } from "react";
import { number, percent, duration, Badge, Empty, Table } from "./shared";
import { reviewTypeCopy } from "../components/review/shared";

const sourceLabels = [
  ["TAP", "Ketuk tombol"],
  ["REPLY", "Balas kartu"],
  ["THREAD_REPLY", "Balas di rantai pesan"],
  ["PLAIN", "Pesan biasa"],
  ["LABEL_TEXT", "Label tombol diketik"],
  ["VIA_ASSISTANT", "Lewat asisten"],
  ["UNKNOWN", "Cara tidak tercatat"],
];

const typeLabel = (type) => reviewTypeCopy[type]?.[0] || "Jenis lain";

const ofCount = (count, of) => `${number(count)} dari ${number(of)}`;
const ratioText = (count, of, rate) => (of > 0 ? `${ofCount(count, of)} (${percent(rate)})` : "Belum ada data");

const UNAVAILABLE = "Belum dapat dimuat";

function measures(w, missing, degraded) {
  const unmeasured = (key) => missing.includes(key);
  const lost = (...names) => names.some((name) => degraded.includes(name));
  const answersTotal = w.answers + w.unusableAnswers;
  const row = (label, target, sections, value, ok) => ({
    label,
    target,
    value: lost(...sections) ? UNAVAILABLE : value,
    ok: lost(...sections) ? null : ok,
  });
  const replyRow = (label, key, count) =>
    row(label, "0", ["events", "repairs"], unmeasured(key) ? "Belum bisa diukur" : number(count), unmeasured(key) ? null : count === 0);
  return [
    row(
      "Review selesai di Telegram, dari yang dikirim sebagai kartu",
      "90% atau lebih",
      ["cards"],
      ratioText(w.finishedTelegram, w.cardsSent, w.finishedTelegramRate),
      w.cardsSent > 0 ? w.finishedTelegramRate >= 0.9 : null,
    ),
    row(
      "Jenis review yang sebagian besar (di atas 70%) selesai di Telegram",
      "Semua jenis",
      ["cards"],
      w.typesSent > 0 ? ofCount(w.typesMostlyTelegram, w.typesSent) : "Belum ada data",
      w.typesSent > 0 ? w.typesMostlyTelegram === w.typesSent : null,
    ),
    row(
      "Jawaban yang tidak bisa dipakai bot",
      "Di bawah 2%",
      ["events"],
      ratioText(w.unusableAnswers, answersTotal, w.unusableAnswerRate),
      answersTotal > 0 ? w.unusableAnswerRate < 0.02 : null,
    ),
    row("Pesan yang sama dikirim lagi dalam 3 menit", "0 sampai 1", ["events"], number(w.repeatedInput), w.repeatedInput <= 1),
    replyRow("Balasan bot rusak atau kosong", "broken_or_empty_replies", w.brokenReplies),
    replyRow("Balasan bot yang menjelaskan cara kerja Telegram", "telegram_mechanics_replies", w.mechanicsReplies),
    row(
      "Waktu tengah dari kartu terkirim sampai selesai, untuk review yang selesai di Telegram",
      "Sekitar 1 menit atau kurang",
      ["median"],
      w.medianFinishMs == null ? "Belum ada data" : duration(w.medianFinishMs),
      w.medianFinishMs == null ? null : w.medianFinishMs <= 60000,
    ),
  ];
}

const verdict = (ok) => (ok == null ? <span>—</span> : <Badge value={ok ? "HEALTHY" : "WARN"} />);

const rangeText = (w) => {
  const fmt = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "Asia/Jakarta" });
  return `${fmt.format(new Date(w.start))} – ${fmt.format(new Date(new Date(w.end).getTime() - 1))}`;
};

const validShape = (value) => (value?.current && value?.previous && Array.isArray(value.notYetMeasurable) && Array.isArray(value.current.types) ? value : null);

export default function InteractionWeekly() {
  const [state, setState] = useState({ loading: true, data: null, status: 0 });
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/v1/operations/interaction-weekly", { signal: controller.signal })
      .then(async (response) => setState({ loading: false, data: response.ok ? validShape(await response.json()) : null, status: response.status }))
      .catch((err) => {
        if (err?.name !== "AbortError") setState({ loading: false, data: null, status: 0 });
      });
    return () => controller.abort();
  }, []);

  const { data, loading, status } = state;
  return (
    <article className="surface admin-panel" aria-labelledby="interaction-weekly-title">
      <div className="section-title">
        <h2 id="interaction-weekly-title">Interaksi Telegram per minggu</h2>
      </div>
      {loading && <Empty>Memuat ringkasan interaksi…</Empty>}
      {!loading && !data && (
        <Empty>
          {status === 403
            ? "Ringkasan interaksi hanya tersedia untuk pemilik rumah tangga."
            : "Ringkasan interaksi belum dapat dimuat."}
        </Empty>
      )}
      {data && <Summary data={data} />}
    </article>
  );
}

function Summary({ data }) {
  const degraded = data.degradedSections || [];
  const current = measures(data.current, data.notYetMeasurable, degraded);
  const previous = measures(data.previous, data.notYetMeasurable, degraded);
  const rows = data.current.types;
  return (
    <>
      <p className="page-intro">
        7 hari terakhir ({rangeText(data.current)}) dibandingkan 7 hari sebelumnya ({rangeText(data.previous)}), menurut tanggal WIB.
      </p>
      <Table headers={["Ukuran", "7 hari terakhir", "Status", "7 hari sebelumnya", "Target"]}>
        {current.map((m, i) => (
          <tr key={m.label}>
            <td>{m.label}</td>
            <td>{m.value}</td>
            <td>{verdict(m.ok)}</td>
            <td>{previous[i].value}</td>
            <td>{m.target}</td>
          </tr>
        ))}
      </Table>
      {data.notYetMeasurable.length > 0 && (
        <p className="notice" role="note">
          Belum bisa diukur: {[
            data.notYetMeasurable.some((k) => k.endsWith("_replies")) && "kualitas balasan bot (belum ada balasan yang tercatat diperbaiki)",
            data.notYetMeasurable.includes("answers_via_assistant") && "jawaban lewat asisten",
          ].filter(Boolean).join(", ")}
          . Angka 0 pada ukuran ini bukan tanda berhasil.
        </p>
      )}
      <div className="section-title"><h3>Cara jawaban masuk</h3></div>
      <Table headers={["Cara", "7 hari terakhir", "7 hari sebelumnya"]}>
        {sourceLabels.map(([key, label]) => {
          const cell = (window) => {
            if (degraded.includes("events")) return UNAVAILABLE;
            if (key === "VIA_ASSISTANT" && data.notYetMeasurable.includes("answers_via_assistant")) return "Belum bisa diukur";
            return number(window.answerSources[key]);
          };
          return (
            <tr key={key}>
              <td>{label}</td>
              <td>{cell(data.current)}</td>
              <td>{cell(data.previous)}</td>
            </tr>
          );
        })}
      </Table>
      <div className="section-title"><h3>Per jenis review (7 hari terakhir)</h3></div>
      {degraded.includes("cards") ? (
        <Empty>{UNAVAILABLE}.</Empty>
      ) : rows.length ? (
        <Table headers={["Jenis review", "Terkirim", "Selesai di Telegram", "Selesai di web", "Masih terbuka", "Lainnya"]}>
          {rows.map((row) => (
            <tr key={row.reviewType}>
              <td>{typeLabel(row.reviewType)}</td>
              <td>{number(row.sent)}</td>
              <td>{number(row.finishedTelegram)}</td>
              <td>{number(row.finishedWeb)}</td>
              <td>{number(row.stillOpen)}</td>
              <td>{number(row.sent - row.finishedTelegram - row.finishedWeb - row.stillOpen)}</td>
            </tr>
          ))}
        </Table>
      ) : (
        <Empty>Belum ada kartu review yang terkirim minggu ini.</Empty>
      )}
    </>
  );
}
