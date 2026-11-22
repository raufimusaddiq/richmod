"use client";

import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { CHART_DRAW_MS, CHART_SETTLE_MS, chartMotionProps, countUpText, datasetKey, parseCountUpText, prefersReducedMotion, startCountUp, subscribeReducedMotion } from "./motion.js";

export function usePrefersReducedMotion() {
  return useSyncExternalStore(subscribeReducedMotion, prefersReducedMotion, () => true);
}

export function useChartMotion(data) {
  const reduced = usePrefersReducedMotion();
  const key = useMemo(() => datasetKey(data), [data]);
  const [settledKey, setSettledKey] = useState(null);
  useEffect(() => {
    if (reduced) return undefined;
    const timer = setTimeout(() => setSettledKey(key), CHART_DRAW_MS + CHART_SETTLE_MS);
    return () => clearTimeout(timer);
  }, [key, reduced]);
  return chartMotionProps(!reduced && settledKey !== key);
}

export function useCountUpText(text) {
  const reduced = usePrefersReducedMotion();
  const parsed = useMemo(() => parseCountUpText(text), [text]);
  const target = parsed ? parsed.value : null;
  const shown = useRef(0);
  const [settled, setSettled] = useState(null);
  const [frame, setFrame] = useState(0);
  const animating = !reduced && parsed != null && settled !== text;
  useEffect(() => {
    if (reduced || target == null) {
      shown.current = target ?? 0;
      return undefined;
    }
    if (shown.current === target) {
      setSettled(text);
      return undefined;
    }
    return startCountUp({
      from: shown.current,
      to: target,
      onFrame: value => { shown.current = value; setFrame(value); },
      onDone: () => setSettled(text),
    });
  }, [text, target, reduced]);
  return { animating, visible: animating ? countUpText(parsed, frame) : text };
}
