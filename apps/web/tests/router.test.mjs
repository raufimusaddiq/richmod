import assert from "node:assert/strict";
import test from "node:test";
import { Suspense, createElement } from "react";
import { renderToString } from "react-dom/server";
import { Link, RouterProvider, usePathname, useSearchParams } from "../app/router/index.js";
import { isPlainLeftClick, navigate, normalizePath } from "../app/router/location.js";
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
    scrollTo(x, y) { calls.push(["scroll", x, y]); },
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

test("navigate pushes a new location, replaces the same one, and scrolls to top only for a new path", () => {
  withBrowser("https://richmod.test/transactions?a=1", calls => {
    navigate("/analytics");
    navigate("/transactions?a=1");
    navigate("/transactions?a=2");
    navigate("/inbox?view=history", { replace: true });
    navigate("https://elsewhere.test/x");
    assert.deepEqual(calls, [
      ["push", "/analytics"], ["scroll", 0, 0],
      ["replace", "/transactions?a=1"],
      ["push", "/transactions?a=2"],
      ["replace", "/inbox?view=history"], ["scroll", 0, 0],
      ["assign", "https://elsewhere.test/x"],
    ]);
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
    assert.deepEqual(calls, [["push", "/inbox"], ["scroll", 0, 0]]);
  });
});
