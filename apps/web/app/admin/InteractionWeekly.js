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
];

const typeLabel = (type) => reviewTypeCopy[type]?.[0] || "Jenis lain";

const ofCount = (count, of) => `${number(count)} dari ${number(of)}`;
const ratioText = (count, of, rate) => (of > 0 ? `${ofCount(count, of)} (${percent(rate)})` : "Belum ada data");

function measures(w, missing) {
  const unmeasured = (key) => missing.includes(key);
  const answersTotal = w.answers + w.unusableAnswers;
  return [
    {
      label: "Review selesai di Telegram, dari yang dikirim sebagai kartu",
      target: "90% atau lebih",
      value: ratioText(w.finishedTelegram, w.cardsSent, w.finishedTelegramRate),
      ok: w.cardsSent > 0 ? w.finishedTelegramRate >= 0.9 : null,
    },
    {
      label: "Jenis review yang sebagian besar (di atas 70%) selesai di Telegram",
      target: "Semua jenis",
      value: w.typesSent > 0 ? ofCount(w.typesMostlyTelegram, w.typesSent) : "Belum ada data",
      ok: w.typesSent > 0 ? w.typesMostlyTelegram === w.typesSent : null,
    },
    {
      label: "Jawaban yang tidak bisa dipakai bot",
      target: "Di bawah 2%",
      value: ratioText(w.unusableAnswers, answersTotal, w.unusableAnswerRate),
      ok: answersTotal > 0 ? w.unusableAnswerRate < 0.02 : null,
    },
    {
      label: "Pesan yang sama dikirim lagi dalam 3 menit",
      target: "0 sampai 1",
      value: number(w.repeatedInput),
      ok: w.repeatedInput <= 1,
    },
    {
      label: "Balasan bot rusak atau kosong",
      target: "0",
      value: unmeasured("broken_or_empty_replies") ? "Belum bisa diukur" : number(w.brokenReplies),
      ok: unmeasured("broken_or_empty_replies") ? null : w.brokenReplies === 0,
    },
    {
      label: "Balasan bot yang menjelaskan cara kerja Telegram",
      target: "0",
      value: unmeasured("telegram_mechanics_replies") ? "Belum bisa diukur" : number(w.mechanicsReplies),
      ok: unmeasured("telegram_mechanics_replies") ? null : w.mechanicsReplies === 0,
    },
    {
      label: "Waktu tengah dari kartu terkirim sampai selesai, untuk review yang selesai di Telegram",
      target: "Sekitar 1 menit atau kurang",
      value: w.medianFinishMs == null ? "Belum ada data" : duration(w.medianFinishMs),
      ok: w.medianFinishMs == null ? null : w.medianFinishMs <= 60000,
    },
  ];
}

const verdict = (ok) => (ok == null ? <span>—</span> : <Badge value={ok ? "HEALTHY" : "WARN"} />);

const rangeText = (w) => {
  const fmt = new Intl.DateTimeFormat("id-ID", { day: "numeric", month: "short", timeZone: "Asia/Jakarta" });
  return `${fmt.format(new Date(w.start))} – ${fmt.format(new Date(new Date(w.end).getTime() - 1))}`;
};

const validShape = (value) => (value?.current && value?.previous && Array.isArray(value.notYetMeasurable) ? value : null);

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
  const current = measures(data.current, data.notYetMeasurable);
  const previous = measures(data.previous, data.notYetMeasurable);
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
          const unmeasured = key === "VIA_ASSISTANT" && data.notYetMeasurable.includes("answers_via_assistant");
          return (
            <tr key={key}>
              <td>{label}</td>
              <td>{unmeasured ? "Belum bisa diukur" : number(data.current.answerSources[key])}</td>
              <td>{unmeasured ? "Belum bisa diukur" : number(data.previous.answerSources[key])}</td>
            </tr>
          );
        })}
      </Table>
      <div className="section-title"><h3>Per jenis review (7 hari terakhir)</h3></div>
      {rows.length ? (
        <Table headers={["Jenis review", "Terkirim", "Selesai di Telegram", "Selesai di web", "Masih terbuka"]}>
          {rows.map((row) => (
            <tr key={row.reviewType}>
              <td>{typeLabel(row.reviewType)}</td>
              <td>{number(row.sent)}</td>
              <td>{number(row.finishedTelegram)}</td>
              <td>{number(row.finishedWeb)}</td>
              <td>{number(row.stillOpen)}</td>
            </tr>
          ))}
        </Table>
      ) : (
        <Empty>Belum ada kartu review yang terkirim minggu ini.</Empty>
      )}
    </>
  );
}
