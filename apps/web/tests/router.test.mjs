import assert from "node:assert/strict";
import test from "node:test";
import { Suspense, createElement } from "react";
import { renderToString } from "react-dom/server";
import { Link, RouterProvider, usePathname, useSearchParams } from "../app/router/index.js";
import { applyPendingScrollReset, isPlainLeftClick, navigate, normalizePath } from "../app/router/location.js";
import { matchRoute, routePaths } from "../app/routes.js";
import { routeMeta } from "../app/lib/siteMeta.js";

function fakeBrowser(url) {
  const current = new URL(url);
  const calls = [];
  const listeners = {};
  const target = {
    location: {
      get origin() { return current.origin; },
      get pathname() { return current.pathname; },
      get search() { return current.search; },
      get hash() { return current.hash; },
      get href() { return current.href; },
      assign(value) { calls.push(["assign", value]); },
    },
    history: {
      pushState(_state, _title, next) { calls.push(["push", next]); },
      replaceState(_state, _title, next) { calls.push(["replace", next]); },
    },
    addEventListener(name, listener) { listeners[name] = listener; },
    removeEventListener() {},
    dispatchEvent() {},
    scrollTo(options) { calls.push(["scroll", options]); },
  };
  return { target, calls };
}

function withBrowser(url, body) {
  const previous = globalThis.window;
  const browser = fakeBrowser(url);
  globalThis.window = browser.target;
  try {
    return body(browser.calls);
  } finally {
    applyPendingScrollReset();
    if (previous === undefined) delete globalThis.window;
    else globalThis.window = previous;
  }
}

test("every route in the table has a component or a redirect, and unknown paths are 404", () => {
  assert.deepEqual(routePaths.sort(), Object.keys(routeMeta).sort());
  for (const path of routePaths) {
    const route = matchRoute(path);
    assert.ok(route.redirect || typeof route.component === "function", path);
    assert.ok(route.title && route.description, path);
  }
  assert.equal(matchRoute("/definitely-missing").title, "Halaman tidak ditemukan · Richmod");
  assert.equal(matchRoute("/definitely-missing").redirect, undefined);
});

test("legacy /actions and /reviews are client redirects into the inbox", () => {
  assert.equal(matchRoute("/actions").redirect, "/inbox?view=actions");
  assert.equal(matchRoute("/reviews").redirect, "/inbox?view=transactions");
});

test("trailing slashes normalise to the route path", () => {
  assert.equal(normalizePath("/transactions/"), "/transactions");
  assert.equal(normalizePath("/"), "/");
});

test("only a plain left click is handled by the router", () => {
  assert.equal(isPlainLeftClick({ button: 0 }), true);
  for (const modifier of ["metaKey", "ctrlKey", "shiftKey", "altKey"]) assert.equal(isPlainLeftClick({ button: 0, [modifier]: true }), false, modifier);
  assert.equal(isPlainLeftClick({ button: 1 }), false);
});

test("navigate pushes a new location and replaces the same one without scrolling itself", () => {
  withBrowser("https://richmod.test/transactions?a=1", calls => {
    navigate("/analytics");
    navigate("/transactions?a=1");
    navigate("/transactions?a=2");
    navigate("/inbox?view=history", { replace: true });
    navigate("https://elsewhere.test/x");
    assert.deepEqual(calls, [
      ["push", "/analytics"],
      ["replace", "/transactions?a=1"],
      ["push", "/transactions?a=2"],
      ["replace", "/inbox?view=history"],
      ["assign", "https://elsewhere.test/x"],
    ]);
  });
});

test("a new path records one pending instant scroll reset that the router applies once", () => {
  withBrowser("https://richmod.test/transactions", calls => {
    assert.equal(applyPendingScrollReset(), false);
    navigate("/analytics");
    assert.deepEqual(calls, [["push", "/analytics"]]);
    assert.equal(applyPendingScrollReset(), true);
    assert.deepEqual(calls.at(-1), ["scroll", { top: 0, left: 0, behavior: "instant" }]);
    assert.equal(applyPendingScrollReset(), false);
    assert.equal(calls.length, 2);
  });
});

test("a same-path or hash navigation leaves the scroll position alone", () => {
  withBrowser("https://richmod.test/transactions", calls => {
    navigate("/transactions?a=2");
    navigate("/transactions#recent");
    navigate("/analytics#where");
    assert.equal(applyPendingScrollReset(), false);
    assert.equal(calls.some(call => call[0] === "scroll"), false);
  });
});

test("the server render never consumes the pending scroll reset", () => {
  withBrowser("https://richmod.test/transactions", calls => {
    navigate("/analytics");
    renderToString(createElement(RouterProvider, { initial: { pathname: "/analytics", search: "" } }, createElement("p", null, "x")));
    assert.equal(calls.some(call => call[0] === "scroll"), false);
    assert.equal(applyPendingScrollReset(), true);
  });
});

test("the server render reports the route's pathname and Link renders a plain anchor", () => {
  const Probe = () => createElement("p", null, usePathname());
  const html = renderToString(createElement(RouterProvider, { initial: { pathname: "/settings", search: "" } }, createElement(Probe), createElement(Link, { href: "/inbox", className: "x" }, "Buka")));
  assert.match(html, /<p>\/settings<\/p>/);
  assert.match(html, /<a href="\/inbox" class="x">Buka<\/a>/);
});

test("useSearchParams renders on the client only, so a Suspense boundary prerenders its fallback", () => {
  const Reader = () => createElement("p", null, useSearchParams().get("tab"));
  const html = renderToString(
    createElement(RouterProvider, { initial: { pathname: "/admin", search: "?tab=jobs" } }, createElement(Suspense, { fallback: createElement("b", null, "Memuat") }, createElement(Reader))),
    { onError() {} },
  );
  assert.match(html, /<b>Memuat<\/b>/);
  assert.doesNotMatch(html, /jobs/);
});

test("Link leaves modified clicks, new-tab targets and other origins to the browser", () => {
  withBrowser("https://richmod.test/", calls => {
    const element = createElement(Link, { href: "/inbox", target: "_blank" }, "x");
    const click = (props, event) => {
      const anchor = Link(props);
      const synthetic = { button: 0, defaultPrevented: false, preventDefault() { this.defaultPrevented = true; }, ...event };
      anchor.props.onClick(synthetic);
      return synthetic.defaultPrevented;
    };
    assert.equal(click({ href: "/inbox" }, {}), true);
    assert.equal(click({ href: "/inbox" }, { metaKey: true }), false);
    assert.equal(click(element.props, {}), false);
    assert.equal(click({ href: "https://elsewhere.test/" }, {}), false);
    assert.equal(click({ href: "/inbox", onClick: event => event.preventDefault() }, {}), true);
    assert.deepEqual(calls, [["push", "/inbox"]]);
  });
});
