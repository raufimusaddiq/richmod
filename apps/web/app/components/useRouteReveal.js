import { useEffect } from "react";

const REVEAL_LIMIT_MS = 15000;

export default function useRouteReveal(ref) {
  useEffect(() => {
    const main = ref.current;
    if (!main || !main.querySelector(".skeleton-page")) return undefined;
    const observer = new MutationObserver(records => {
      for (const record of records) {
        for (const node of record.addedNodes) {
          if (node instanceof HTMLElement && node.isConnected && !node.matches(".skeleton-page, .toast, [role='alert']") && !node.closest("[data-reveal], select")) node.setAttribute("data-reveal", "");
        }
      }
      if (!main.querySelector(".skeleton-page")) observer.disconnect();
    });
    observer.observe(main, { childList: true, subtree: true });
    const timer = window.setTimeout(() => observer.disconnect(), REVEAL_LIMIT_MS);
    return () => { observer.disconnect(); window.clearTimeout(timer); };
  }, [ref]);
}
