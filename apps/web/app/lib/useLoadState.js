"use client";

import { useCallback, useEffect, useRef, useState } from "react";

// Explicit load states for a page or a drawer. A page that "has a boolean
// loading" cannot tell a first load from a reload, so a reload swaps the whole
// tree for a skeleton and unmounts any form the user is typing in.
//
//   idle       nothing requested yet
//   loading    first request, nothing to show yet: a skeleton is right
//   refreshing a reload; already-rendered children stay mounted
//   error      the last request failed; `error` holds the message
//   ready      the last request succeeded
export const LOAD = Object.freeze({ IDLE: "idle", LOADING: "loading", REFRESHING: "refreshing", ERROR: "error", READY: "ready" });

// A reload keeps children mounted; only a load with nothing shown yet may skeleton.
export const startingStatus = hasLoaded => (hasLoaded ? LOAD.REFRESHING : LOAD.LOADING);

const isAbort = error => error?.name === "AbortError";

// A failure whose message is written for the household. Anything else thrown
// (a TypeError from fetch, a JSON parse error) is browser jargon, so the hook
// shows the caller's fallback instead of the raw message.
export class LoadError extends Error {}
const shown = (failure, fallback) => (failure instanceof LoadError && failure.message ? failure.message : fallback);

// Runs `loader({ signal, current })` with a staleness guard: starting a new run
// aborts the previous one, and a superseded run can neither set state nor
// report an error. The loader must apply its results only while `current()`.
// Throw to fail; the message becomes `error`. `onFailure` lets a page drop data
// that should not stay under the error banner.
export function useGuardedLoad(loader, { onFailure, fallback = "Data belum dapat dimuat. Coba lagi." } = {}) {
  const [status, setStatus] = useState(LOAD.IDLE);
  const [error, setError] = useState("");
  const [hasLoaded, setHasLoaded] = useState(false);
  const sequence = useRef(0), controller = useRef(null), loaded = useRef(false);
  const loaderRef = useRef(loader), failureRef = useRef(onFailure);
  loaderRef.current = loader; failureRef.current = onFailure;

  const run = useCallback(async (...args) => {
    controller.current?.abort();
    const own = new AbortController();
    controller.current = own;
    const id = ++sequence.current;
    const current = () => sequence.current === id && !own.signal.aborted;
    setError(""); setStatus(startingStatus(loaded.current));
    try {
      await loaderRef.current({ signal: own.signal, current }, ...args);
      if (!current()) return false;
      loaded.current = true; setHasLoaded(true); setStatus(LOAD.READY);
      return true;
    } catch (failure) {
      if (!current() || isAbort(failure)) return false;
      setError(shown(failure, fallback));
      setStatus(LOAD.ERROR);
      failureRef.current?.(failure);
      return false;
    }
  }, []);

  useEffect(() => () => { sequence.current += 1; controller.current?.abort(); }, []);

  return { status, error, hasLoaded, run, loading: status === LOAD.LOADING, refreshing: status === LOAD.REFRESHING, failed: status === LOAD.ERROR };
}

// A drawer that opens on one record: {closed | loading | error | ready, id, data}.
// Opening or closing aborts the previous request, so a slow response for record
// A can never repaint a drawer the user closed or pointed at record B.
export function useDrawerRecord(fetchRecord, fallback = "Detail belum dapat dimuat. Coba lagi.") {
  const [state, setState] = useState({ status: "closed", id: null, data: null, error: "" });
  const controller = useRef(null), sequence = useRef(0), fetchRef = useRef(fetchRecord);
  fetchRef.current = fetchRecord;

  const close = useCallback(() => {
    sequence.current += 1; controller.current?.abort();
    setState({ status: "closed", id: null, data: null, error: "" });
  }, []);

  const open = useCallback(async (id, seed = null) => {
    controller.current?.abort();
    const own = new AbortController();
    controller.current = own;
    const ticket = ++sequence.current;
    setState({ status: "loading", id, data: seed, error: "" });
    try {
      const data = await fetchRef.current(id, own.signal);
      if (sequence.current !== ticket || own.signal.aborted) return;
      setState({ status: "ready", id, data, error: "" });
    } catch (failure) {
      if (sequence.current !== ticket || own.signal.aborted || isAbort(failure)) return;
      setState({ status: "error", id, data: seed, error: shown(failure, fallback) });
    }
  }, []);

  useEffect(() => () => { sequence.current += 1; controller.current?.abort(); }, []);

  return { ...state, open, close, retry: () => state.id != null && open(state.id, state.data) };
}

// One busy flag per key (a card id, a setting name) instead of a single string
// that card B overwrites while card A is still saving. `run` refuses a second
// start for the same key synchronously, so a double click cannot submit twice,
// and always releases the key in `finally`.
export function useBusyMap() {
  const [busy, setBusy] = useState({});
  const live = useRef(new Set());
  const run = useCallback(async (key, task) => {
    if (live.current.has(key)) return undefined;
    live.current.add(key);
    setBusy(current => ({ ...current, [key]: true }));
    try {
      return await task();
    } finally {
      live.current.delete(key);
      setBusy(({ [key]: _released, ...rest }) => rest);
    }
  }, []);
  return { busy, run, isBusy: key => Boolean(busy[key]) };
}

// Read the server's { error } message out of a failed response.
export async function responseError(response, fallback) {
  const body = await response.json().catch(() => ({}));
  return body?.error || fallback;
}
