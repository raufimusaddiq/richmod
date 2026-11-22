import { createContext, createElement, startTransition, useContext, useEffect, useLayoutEffect, useMemo, useState } from "react";
import { applyPendingScrollReset, installHistoryNotifier, isPlainLeftClick, navigate, normalizePath, readLocation, subscribeToLocation } from "./location";

const LocationContext = createContext({ pathname: "/", search: "" });

export class ClientOnlyError extends Error {
  constructor() {
    super("useSearchParams needs the browser location and renders on the client only");
    this.name = "ClientOnlyError";
  }
}

export function RouterProvider({ initial = { pathname: "/", search: "" }, children }) {
  const [location, setLocation] = useState(() => readLocation(initial));

  useEffect(() => {
    installHistoryNotifier();
    const sync = () => startTransition(() => setLocation(previous => {
      const next = readLocation(initial);
      return previous.pathname === next.pathname && previous.search === next.search ? previous : next;
    }));
    sync();
    return subscribeToLocation(sync);
  }, []);

  useLayoutEffect(() => { applyPendingScrollReset(); }, [location]);

  const value = useMemo(() => ({ pathname: normalizePath(location.pathname), search: location.search }), [location.pathname, location.search]);
  return createElement(LocationContext.Provider, { value }, children);
}

export function usePathname() {
  return useContext(LocationContext).pathname;
}

export function useSearchParams() {
  const { search } = useContext(LocationContext);
  const params = useMemo(() => new URLSearchParams(search), [search]);
  if (typeof window === "undefined") throw new ClientOnlyError();
  return params;
}

const routerApi = {
  push: href => navigate(href),
  replace: href => navigate(href, { replace: true }),
  back: () => window.history.back(),
  forward: () => window.history.forward(),
};

export function useRouter() {
  return routerApi;
}

export function Link({ href, replace = false, onClick, target, children, ...rest }) {
  function handleClick(event) {
    onClick?.(event);
    if (event.defaultPrevented || !isPlainLeftClick(event)) return;
    if ((target && target !== "_self") || rest.download !== undefined) return;
    const url = new URL(href, window.location.href);
    if (url.origin !== window.location.origin) return;
    event.preventDefault();
    navigate(href, { replace });
  }
  return createElement("a", { href, target, onClick: handleClick, ...rest }, children);
}

export function useDocumentMeta(title, description) {
  useEffect(() => {
    if (title) document.title = title;
    if (description) document.querySelector('meta[name="description"]')?.setAttribute("content", description);
  }, [title, description]);
}
